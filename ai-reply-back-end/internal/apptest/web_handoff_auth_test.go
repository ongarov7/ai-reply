package apptest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

func TestWebHandoffHashTTLAndIndependentNativeSession(t *testing.T) {
	h := newHarness(t)
	native := h.signIn("handoff-native@example.com")
	ctx := context.Background()
	code, err := h.authSvc.CreateWebPaymentHandoff(ctx, native.userID, "ru")
	if err != nil || len(code.Code) != 43 || code.ExpiresIn != 60 {
		t.Fatalf("handoff creation: %v", err)
	}
	digest := sha256.Sum256([]byte(code.Code))
	if got := h.text(`SELECT code_hash FROM web_auth_handoffs`); got != hex.EncodeToString(digest[:]) || got == code.Code {
		t.Fatal("raw capability was stored")
	}
	user, err := h.authSvc.PeekWebPaymentHandoff(ctx, code.Code)
	if err != nil || user.ID != native.userID {
		t.Fatal("peek changed the account")
	}
	_, claims, err := h.authSvc.Authenticate(ctx, native.access)
	if err != nil {
		t.Fatal(err)
	}
	web, err := h.authSvc.ConsumeWebPaymentHandoff(ctx, code.Code, auth.DeviceInfo{Platform: domain.PlatformIOS, DeviceID: claims.DeviceID})
	if err != nil || web.User.ID != native.userID || web.DeviceID == claims.DeviceID {
		t.Fatalf("web exchange: %v", err)
	}
	device, err := h.store.Device(ctx, web.DeviceID)
	if err != nil || device.Platform != domain.PlatformWeb {
		t.Fatal("handoff reused a native device")
	}
	if _, err = h.authSvc.ConsumeWebPaymentHandoff(ctx, code.Code, auth.DeviceInfo{}); !errors.Is(err, auth.ErrWebHandoffInvalid) {
		t.Fatal("replayed handoff was accepted")
	}
	if _, err = h.authSvc.LogoutSession(ctx, web.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err = h.authSvc.Refresh(ctx, native.refresh, auth.DeviceInfo{Platform: domain.PlatformIOS}); err != nil {
		t.Fatal("web logout revoked the native session")
	}
	expiring, err := h.authSvc.CreateWebPaymentHandoff(ctx, native.userID, "en")
	if err != nil {
		t.Fatal(err)
	}
	h.clock.T = h.clock.T.Add(auth.WebPaymentHandoffTTL)
	if _, err = h.authSvc.PeekWebPaymentHandoff(ctx, expiring.Code); !errors.Is(err, auth.ErrWebHandoffInvalid) {
		t.Fatal("handoff valid at expiry boundary")
	}
	if _, err = h.authSvc.ConsumeWebPaymentHandoff(ctx, expiring.Code, auth.DeviceInfo{}); !errors.Is(err, auth.ErrWebHandoffInvalid) {
		t.Fatal("expired handoff consumed")
	}
}

func TestWebHandoffConcurrentConsumeAndTamper(t *testing.T) {
	h := newHarness(t)
	u := h.signIn("handoff-concurrent@example.com")
	ctx := context.Background()
	code, err := h.authSvc.CreateWebPaymentHandoff(ctx, u.userID, "kk")
	if err != nil {
		t.Fatal(err)
	}
	first := "A"
	if code.Code[0] == 'A' {
		first = "B"
	}
	if _, err = h.authSvc.ConsumeWebPaymentHandoff(ctx, first+code.Code[1:], auth.DeviceInfo{}); !errors.Is(err, auth.ErrWebHandoffInvalid) {
		t.Fatal("tampered code accepted")
	}
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.authSvc.ConsumeWebPaymentHandoff(ctx, code.Code, auth.DeviceInfo{})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, auth.ErrWebHandoffInvalid) {
			t.Error(err)
		}
	}
	if winners != 1 || h.scalar(`SELECT count(*) FROM devices WHERE user_id=? AND platform='web'`, u.userID) != 1 {
		t.Fatal("single-use handoff created multiple browser sessions")
	}
	if h.scalar(`SELECT count(*) FROM billing_orders`) != 0 {
		t.Fatal("handoff created a payment")
	}
}

func TestWebHandoffRejectsOtherEnvironmentDisabledAndDeletedAccounts(t *testing.T) {
	h := newHarness(t)
	u := h.signIn("handoff-isolated@example.com")
	ctx := context.Background()
	code, err := h.authSvc.CreateWebPaymentHandoff(ctx, u.userID, "en")
	if err != nil {
		t.Fatal(err)
	}
	other := newHarness(t)
	if _, err = other.authSvc.ConsumeWebPaymentHandoff(ctx, code.Code, auth.DeviceInfo{}); !errors.Is(err, auth.ErrWebHandoffInvalid) {
		t.Fatal("capability crossed databases")
	}
	if _, err = h.db.Writer().Exec(`UPDATE users SET status='disabled' WHERE id=?`, u.userID); err != nil {
		t.Fatal(err)
	}
	if _, err = h.authSvc.PeekWebPaymentHandoff(ctx, code.Code); !errors.Is(err, auth.ErrWebHandoffInvalid) {
		t.Fatal("disabled user code remained valid")
	}
	if _, err = h.authSvc.CreateWebPaymentHandoff(ctx, u.userID, "en"); !errors.Is(err, auth.ErrWebHandoffInvalid) {
		t.Fatal("disabled user created a code")
	}
	if _, err = h.db.Writer().Exec(`DELETE FROM users WHERE id=?`, u.userID); err != nil {
		t.Fatal(err)
	}
	if h.scalar(`SELECT count(*) FROM web_auth_handoffs`) != 0 {
		t.Fatal("account deletion retained handoff capabilities")
	}
	if _, err = h.authSvc.ConsumeWebPaymentHandoff(ctx, code.Code, auth.DeviceInfo{}); !errors.Is(err, auth.ErrWebHandoffInvalid) {
		t.Fatal("deleted user authenticated")
	}
}

func TestWebHandoffExpiresWhileWaitingForConsumeTransaction(t *testing.T) {
	h := newHarness(t)
	u := h.signIn("handoff-lock@example.com")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	code, err := h.authSvc.CreateWebPaymentHandoff(ctx, u.userID, "ru")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := h.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	waits := h.db.Writer().Stats().WaitCount
	done := make(chan error, 1)
	go func() { _, err := h.authSvc.ConsumeWebPaymentHandoff(ctx, code.Code, auth.DeviceInfo{}); done <- err }()
	deadline := time.Now().Add(time.Second)
	for h.db.Writer().Stats().WaitCount == waits && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if h.db.Writer().Stats().WaitCount == waits {
		t.Fatal("consume did not wait for the transaction")
	}
	h.clock.T = h.clock.T.Add(auth.WebPaymentHandoffTTL)
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, auth.ErrWebHandoffInvalid) {
		t.Fatal("expired capability consumed after the writer lock")
	}
}
