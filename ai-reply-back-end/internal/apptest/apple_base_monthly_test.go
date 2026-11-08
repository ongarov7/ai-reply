package apptest

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/iap/appstore"
	"github.com/aireply/ai-reply-back-end/internal/iap/appstore/appstoretest"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

func monthlyBaseHarness(t *testing.T) *harness {
	h := newHarness(t, withEnv("TEST_APPROVED_ENTITLEMENTS", "true"), withIAP(t))
	h.enableApprovedProducts()
	// The deterministic test clock predates the real migration timestamp.
	h.exec(`UPDATE system_settings SET value='0' WHERE key='apple_base_monthly_since'`)
	h.exec(`UPDATE iap_products SET is_active=0 WHERE code='base_yearly'`)
	return h
}

func TestAppleMonthlyBaseAccumulatesAndRemainsSpendableAfterExpiry(t *testing.T) {
	h := monthlyBaseHarness(t)
	s := h.signIn("monthly-rollover@example.com")
	start := h.clock.Now()
	end := start.AddDate(0, 1, 0)
	first := h.subscriptionTx(s.userID, "monthly-1", "monthly-chain", "base_monthly", start, end, nil)
	mustStatus(t, h.postApple(s.access, first), http.StatusOK, "")
	mustStatus(t, h.postApple(s.access, first), http.StatusOK, "")
	if left := h.creditsLeft(s.access); left != 1000 {
		t.Fatalf("initial balance: %v", left)
	}
	for i := 0; i < 41; i++ {
		mustStatus(t, h.generate(s.access, "Сәлем"), http.StatusOK, "")
	}
	h.clock.Advance(24 * time.Hour)
	s = h.refresh(s)
	if left := h.creditsLeft(s.access); left != 959 {
		t.Fatalf("daily reset changed balance: %v", left)
	}
	h.clock.Advance(end.Sub(h.clock.Now()))
	s = h.refresh(s)
	renewal := h.subscriptionTx(s.userID, "monthly-2", "monthly-chain", "base_monthly", end, end.AddDate(0, 1, 0), nil)
	mustStatus(t, h.appleNotification("DID_RENEW", "monthly-renewal", renewal), http.StatusOK, "")
	mustStatus(t, h.postApple(s.access, renewal), http.StatusOK, "")
	if left := h.creditsLeft(s.access); left != 1959 {
		t.Fatalf("renewal did not carry remainder: %v", left)
	}
	h.clock.Advance(400 * 24 * time.Hour)
	s, _ = h.signInOn("monthly-rollover@example.com", "android")
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	mustStatus(t, usage, http.StatusOK, "")
	if usage.num("daily_limit") != 0 || usage.num("monthly_limit") != 0 || usage.num("purchased_credits") != 1959 || usage.body["credits_never_expire"] != true {
		t.Fatalf("expired usage: %s", usage.raw)
	}
	mustStatus(t, h.generate(s.access, "Қалған жауап"), http.StatusOK, "")
	if left := h.creditsLeft(s.access); left != 1958 {
		t.Fatalf("expired credits not spendable: %v", left)
	}
	if n := h.scalar(`SELECT count(*) FROM credit_grants WHERE user_id=? AND product_code='apple_base_monthly' AND expires_at IS NULL`, s.userID); n != 2 {
		t.Fatalf("grants: %d", n)
	}
	if n := h.scalar(`SELECT count(*) FROM apple_base_credit_days WHERE user_id=?`, s.userID); n != 0 {
		t.Fatalf("monthly purchase also accrued daily replies: %d", n)
	}
	if sum := h.scalar(`SELECT sum(delta) FROM credit_ledger WHERE user_id=?`, s.userID); sum != 1958 {
		t.Fatalf("ledger: %d", sum)
	}
}

func TestAppleMonthlyBaseRefundReversalRestoresOnlyUnspentCredits(t *testing.T) {
	h := monthlyBaseHarness(t)
	s := h.signIn("monthly-refund@example.com")
	start := h.clock.Now()
	end := start.AddDate(0, 1, 0)
	signed := h.subscriptionTx(s.userID, "monthly-refund", "monthly-refund-chain", "base_monthly", start, end, nil)
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	for i := 0; i < 7; i++ {
		mustStatus(t, h.generate(s.access, "Сәлем"), http.StatusOK, "")
	}
	h.clock.Advance(time.Minute)
	refund := h.subscriptionTx(s.userID, "monthly-refund", "monthly-refund-chain", "base_monthly", start, end, func(tx *appstoretest.Transaction) { tx.RevocationDate = h.clock.Now().UnixMilli() })
	mustStatus(t, h.appleNotification("REFUND", "monthly-refund-event", refund), http.StatusOK, "")
	if left := h.scalar(`SELECT coalesce(sum(units_remaining),0) FROM credit_grants WHERE user_id=? AND status='active'`, s.userID); left != 0 {
		t.Fatalf("refund did not remove remainder atomically: %d", left)
	}
	h.clock.Advance(time.Minute)
	reversed := h.subscriptionTx(s.userID, "monthly-refund", "monthly-refund-chain", "base_monthly", start, end, nil)
	mustStatus(t, h.appleNotification("REFUND_REVERSED", "monthly-reversed", reversed), http.StatusOK, "")
	mustStatus(t, h.appleNotification("REFUND_REVERSED", "monthly-reversed-again", reversed), http.StatusOK, "")
	// A delayed old refund and a replay cannot revoke the newer reversal or mint replies.
	stale := appstoretest.Notification{NotificationType: "REFUND", NotificationUUID: "monthly-stale-refund", SignedDate: start.Add(time.Minute).UnixMilli(), Version: "2.0", Data: appstoretest.NotificationData{BundleID: h.cfg.IAP.AppleBundleID, Environment: "Production", SignedTransactionInfo: refund}}
	mustStatus(t, h.do(http.MethodPost, "/api/v1/iap/apple/notifications", map[string]any{"signedPayload": h.apple.Sign(t, stale)}, nil), http.StatusOK, "")
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	if left := h.creditsLeft(s.access); left != 993 {
		t.Fatalf("reversal reminted spent credits: %v", left)
	}
	if sum := h.scalar(`SELECT sum(delta) FROM credit_ledger WHERE user_id=?`, s.userID); sum != 993 {
		t.Fatalf("ledger: %d", sum)
	}
}

func TestAppleMonthlyBaseParallelDeliveryAndCatalogChangeCannotDuplicateGrant(t *testing.T) {
	h := monthlyBaseHarness(t)
	s := h.signIn("monthly-concurrent@example.com")
	now := h.clock.Now()
	end := now.AddDate(0, 1, 0)
	in := repository.AppleSubscription{UserID: s.userID, OriginalTransactionID: "monthly-concurrent-chain", TransactionID: "monthly-concurrent", ProductID: "test.base_monthly", Environment: "Production", PurchasedAt: now, ExpiresAt: end, PaidExpiresAt: end, SignedAt: now}
	if err := h.db.Reader().QueryRow(`SELECT id FROM plans WHERE code='base_monthly'`).Scan(&in.PlanID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.store.ApplyAppleSubscription(context.Background(), in, now)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	h.exec(`UPDATE plans SET monthly_message_limit=9000 WHERE code='base_monthly'`)
	if _, err := h.store.ApplyAppleSubscription(context.Background(), in, now); err != nil {
		t.Fatal(err)
	}
	if left := h.creditsLeft(s.access); left != 1000 {
		t.Fatalf("replay changed sold quantity: %v", left)
	}
	if n := h.scalar(`SELECT count(*) FROM credit_grants WHERE user_id=? AND product_code='apple_base_monthly'`, s.userID); n != 1 {
		t.Fatalf("duplicate grants: %d", n)
	}
	h.provider.err = domain.ErrProviderDown
	mustStatus(t, h.generate(s.access, "Қате"), http.StatusBadGateway, "AI_PROVIDER_UNAVAILABLE")
	if left := h.creditsLeft(s.access); left != 1000 {
		t.Fatalf("provider failure spent a reply: %v", left)
	}
}

func TestAppleMonthlyBaseKeepsEarnedCreditsAfterProUpgradeAndCancellation(t *testing.T) {
	h := monthlyBaseHarness(t)
	s := h.signIn("monthly-upgrade@example.com")
	start := h.clock.Now()
	end := start.AddDate(0, 1, 0)
	base := h.subscriptionTx(s.userID, "monthly-base", "monthly-upgrade-chain", "base_monthly", start, end, nil)
	mustStatus(t, h.postApple(s.access, base), http.StatusOK, "")
	h.clock.Advance(time.Hour)
	s = h.refresh(s)
	pro := h.subscriptionTx(s.userID, "monthly-pro", "monthly-upgrade-chain", "pro_monthly", h.clock.Now(), end, nil)
	mustStatus(t, h.postApple(s.access, pro), http.StatusOK, "")
	mustStatus(t, h.generate(s.access, "Pro"), http.StatusOK, "")
	if left := h.creditsLeft(s.access); left != 1000 {
		t.Fatalf("Pro spent Base balance: %v", left)
	}
	renewal := appstore.Renewal{OriginalTransactionID: "monthly-upgrade-chain", Environment: "Production", AutoRenewStatus: 0, SignedDate: h.clock.Now().UnixMilli()}
	event := appstoretest.Notification{NotificationType: "DID_CHANGE_RENEWAL_STATUS", NotificationUUID: "monthly-cancel", SignedDate: h.clock.Now().UnixMilli(), Version: "2.0", Data: appstoretest.NotificationData{BundleID: h.cfg.IAP.AppleBundleID, Environment: "Production", SignedTransactionInfo: pro, SignedRenewalInfo: h.apple.Sign(t, renewal)}}
	mustStatus(t, h.do(http.MethodPost, "/api/v1/iap/apple/notifications", map[string]any{"signedPayload": h.apple.Sign(t, event)}, nil), http.StatusOK, "")
	h.clock.Advance(400 * 24 * time.Hour)
	s = h.signIn("monthly-upgrade@example.com")
	mustStatus(t, h.generate(s.access, "Пайдалану"), http.StatusOK, "")
	if left := h.creditsLeft(s.access); left != 999 {
		t.Fatalf("cancellation removed Base credits: %v", left)
	}
}

func TestAppleMonthlyBaseHistoricalDailyTransactionCannotBecomeMonthlyOnReplay(t *testing.T) {
	h := monthlyBaseHarness(t)
	s := h.signIn("monthly-legacy@example.com")
	start := h.clock.Now()
	end := start.AddDate(0, 1, 0)
	h.exec(`UPDATE system_settings SET value=? WHERE key='apple_base_monthly_since'`, start.Add(time.Hour).UnixMilli())
	old := h.subscriptionTx(s.userID, "monthly-old-daily", "monthly-legacy-chain", "base_monthly", start, end, nil)
	mustStatus(t, h.postApple(s.access, old), http.StatusOK, "")
	if left := h.creditsLeft(s.access); left != 40 {
		t.Fatalf("historical paid quantity changed: %v", left)
	}
	h.exec(`UPDATE system_settings SET value='0' WHERE key='apple_base_monthly_since'`)
	mustStatus(t, h.postApple(s.access, old), http.StatusOK, "")
	if left := h.creditsLeft(s.access); left != 40 {
		t.Fatalf("daily replay minted monthly balance: %v", left)
	}
}
