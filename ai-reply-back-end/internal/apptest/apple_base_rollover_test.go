package apptest

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/iap/appstore"
	"github.com/aireply/ai-reply-back-end/internal/iap/appstore/appstoretest"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

func TestAppleBaseRolloverSurvivesOfflineDaysAndExpiry(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableApprovedProducts()
	s := h.signIn("rollover-expiry@example.com")
	start := h.clock.Now()
	local := start.In(h.cfg.App.Location())
	end := time.Date(local.Year(), local.Month(), local.Day()+3, 0, 0, 0, 0, h.cfg.App.Location())
	signed := h.subscriptionTx(s.userID, "rollover-expiry", "rollover-chain", "base_monthly", start, end, nil)
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	for i := 0; i < 7; i++ {
		r := h.generate(s.access, "Сәлем")
		mustStatus(t, r, http.StatusOK, "")
		if r.num("usage", "daily_limit") != 0 || r.num("usage", "daily_credit_grant") != 40 || r.str("usage", "bucket") != "pack" {
			t.Fatalf("rollover usage: %s", r.raw)
		}
	}
	h.clock.Advance(26 * time.Hour)
	s = h.refresh(s)
	if left := h.creditsLeft(s.access); left != 73 {
		t.Fatalf("day2 balance %v, want73", left)
	}
	h.clock.Advance(400 * 24 * time.Hour)
	s, _ = h.signInOn("rollover-expiry@example.com", "android")
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	mustStatus(t, usage, http.StatusOK, "")
	if usage.num("purchased_credits") != 113 || usage.num("daily_credit_grant") != 0 || usage.num("daily_limit") != 0 {
		t.Fatalf("expired balance: %s", usage.raw)
	}
	mustStatus(t, h.generate(s.access, "Әлі жарамды"), http.StatusOK, "")
	if h.creditsLeft(s.access) != 112 {
		t.Fatal("expired subscription credits cannot be spent")
	}
	if count := h.scalar(`SELECT COUNT(*) FROM apple_base_credit_days WHERE user_id=?`, s.userID); count != 3 {
		t.Fatalf("exact midnight expiry granted %d days, want3", count)
	}
	if count := h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE user_id=? AND expires_at IS NOT NULL`, s.userID); count != 0 {
		t.Fatal("subscription grants have expiry")
	}
	if sum := h.scalar(`SELECT SUM(delta) FROM credit_ledger WHERE user_id=?`, s.userID); sum != 112 {
		t.Fatalf("ledger balance %d", sum)
	}
}

func TestAppleBaseRolloverRefundAndReversalKeepSpentRepliesSpent(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableApprovedProducts()
	s := h.signIn("rollover-refund@example.com")
	start := h.clock.Now()
	local := start.In(h.cfg.App.Location())
	end := time.Date(local.Year(), local.Month(), local.Day()+2, 0, 0, 0, 0, h.cfg.App.Location())
	first := h.subscriptionTx(s.userID, "rollover-refund-1", "rollover-refund-chain", "base_monthly", start, end, nil)
	mustStatus(t, h.postApple(s.access, first), http.StatusOK, "")
	for i := 0; i < 5; i++ {
		mustStatus(t, h.generate(s.access, "Сәлем"), http.StatusOK, "")
	}
	renewed := h.subscriptionTx(s.userID, "rollover-refund-2", "rollover-refund-chain", "base_monthly", end, end.Add(2*24*time.Hour), nil)
	mustStatus(t, h.appleNotification("DID_RENEW", "rollover-renew", renewed), http.StatusOK, "")
	h.clock.Advance(3 * 24 * time.Hour)
	s = h.refresh(s)
	if h.creditsLeft(s.access) != 155 {
		t.Fatalf("renewal before refund %v", h.creditsLeft(s.access))
	}
	refund := h.subscriptionTx(s.userID, "rollover-refund-1", "rollover-refund-chain", "base_monthly", start, end, func(tx *appstoretest.Transaction) { tx.RevocationDate = h.clock.Now().UnixMilli() })
	mustStatus(t, h.appleNotification("REFUND", "rollover-refund", refund), http.StatusOK, "")
	// Revocation commits before another read/accrual call.
	if left := h.scalar(`SELECT COALESCE(SUM(units_remaining),0) FROM credit_grants WHERE user_id=? AND status='active'`, s.userID); left != 80 {
		t.Fatalf("refund left %d, want80", left)
	}
	h.clock.Advance(time.Minute)
	reversal := h.subscriptionTx(s.userID, "rollover-refund-1", "rollover-refund-chain", "base_monthly", start, end, nil)
	mustStatus(t, h.appleNotification("REFUND_REVERSED", "rollover-reversed", reversal), http.StatusOK, "")
	if h.creditsLeft(s.access) != 155 {
		t.Fatal("reversal reminted already spent replies")
	}
	mustStatus(t, h.appleNotification("REFUND_REVERSED", "rollover-reversed-again", reversal), http.StatusOK, "")
	mustStatus(t, h.postApple(s.access, first), http.StatusOK, "")
	if h.creditsLeft(s.access) != 155 {
		t.Fatal("replay changed permanent balance")
	}
}

func TestAppleBaseRolloverRefundsLegacyInflightAndProviderFailures(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableApprovedProducts()
	s := h.signIn("rollover-inflight@example.com")
	now := h.clock.Now()
	date, month := h.todayKeys()
	reservation, err := h.store.ReserveGeneration(context.Background(), repository.ReserveInput{UserID: s.userID, Mode: "reply", Date: date, Month: month, DailyLimit: 40, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	signed := h.subscriptionTx(s.userID, "rollover-inflight", "rollover-inflight-chain", "base_monthly", now.Add(-time.Hour), now.Add(24*time.Hour), nil)
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	if h.creditsLeft(s.access) != 39 {
		t.Fatal("legacy paid charge not subtracted")
	}
	ok, err := h.store.RefundGeneration(context.Background(), reservation.ID, now)
	if err != nil || !ok {
		t.Fatalf("refund: %v %v", ok, err)
	}
	if h.creditsLeft(s.access) != 40 {
		t.Fatal("inflight failed charge did not refund permanent reply")
	}
	h.provider.err = domain.ErrProviderDown
	mustStatus(t, h.generate(s.access, "Сәтсіз"), http.StatusBadGateway, "AI_PROVIDER_UNAVAILABLE")
	if h.creditsLeft(s.access) != 40 {
		t.Fatal("provider failure consumed permanent reply")
	}
}

func TestAppleBaseRolloverConcurrentAccrualAndSpending(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableApprovedProducts()
	s := h.signIn("rollover-concurrent@example.com")
	now := h.clock.Now()
	signed := h.subscriptionTx(s.userID, "rollover-concurrent", "rollover-concurrent-chain", "base_monthly", now, now.Add(24*time.Hour), nil)
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := h.store.AccrueAppleBaseCredits(context.Background(), s.userID, now, h.cfg.App.Location())
			errs <- err
		}()
	}
	wg.Wait()
	for i := 0; i < 20; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if h.scalar(`SELECT COUNT(*) FROM apple_base_credit_days WHERE user_id=?`, s.userID) != 1 {
		t.Fatal("parallel accrual duplicated grant")
	}
	date, month := h.todayKeys()
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.store.ReserveGeneration(context.Background(), repository.ReserveInput{UserID: s.userID, Mode: "reply", Date: date, Month: month, Now: now})
			errs <- err
		}()
	}
	wg.Wait()
	success, limited := 0, 0
	for i := 0; i < 50; i++ {
		err := <-errs
		if err == nil {
			success++
		} else if errors.Is(err, domain.ErrDailyLimit) {
			limited++
		} else {
			t.Fatal(err)
		}
	}
	if success != 40 || limited != 10 || h.creditsLeft(s.access) != 0 {
		t.Fatalf("success%d limited%d balance%v", success, limited, h.creditsLeft(s.access))
	}
}

func TestAppleBaseRolloverRenewalSameDateDoesNotDuplicateAndDoesNotAffectLegacyBase(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableApprovedProducts()
	s := h.signIn("rollover-renewal-day@example.com")
	now := h.clock.Now()
	first := h.subscriptionTx(s.userID, "rollover-same-1", "rollover-same-chain", "base_monthly", now, now.Add(time.Hour), nil)
	next := h.subscriptionTx(s.userID, "rollover-same-2", "rollover-same-chain", "base_monthly", now.Add(time.Hour), now.Add(2*time.Hour), nil)
	mustStatus(t, h.postApple(s.access, first), http.StatusOK, "")
	h.clock.Advance(time.Hour)
	s = h.refresh(s)
	for i := 0; i < 2; i++ {
		mustStatus(t, h.postApple(s.access, next), http.StatusOK, "")
	}
	if h.creditsLeft(s.access) != 40 {
		t.Fatal("renewal/restore same date duplicated40")
	}
	legacy := h.signIn("legacy-base-unchanged@example.com")
	h.assignPlan(h.signInAdmin(), legacy.userID, "base_monthly", "")
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(legacy.access))
	if usage.num("daily_limit") != 40 || usage.body["credits_never_expire"] != false {
		t.Fatalf("legacy contract changed: %s", usage.raw)
	}
}

func TestAppleBaseRolloverKeepsVerifiedBillingGraceAccess(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableApprovedProducts()
	s := h.signIn("rollover-grace@example.com")
	start := h.clock.Now()
	local := start.In(h.cfg.App.Location())
	end := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, h.cfg.App.Location())
	signed := h.subscriptionTx(s.userID, "rollover-grace", "rollover-grace-chain", "base_monthly", start, end, nil)
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	h.clock.Advance(end.Sub(start) + time.Hour)
	s = h.refresh(s)
	signed = h.subscriptionTx(s.userID, "rollover-grace", "rollover-grace-chain", "base_monthly", start, end, nil)
	grace := end.Add(2 * 24 * time.Hour)
	renewal := appstore.Renewal{OriginalTransactionID: "rollover-grace-chain", Environment: "Production", AutoRenewStatus: 1,
		GracePeriodExpiresDate: grace.UnixMilli(), SignedDate: h.clock.Now().UnixMilli()}
	event := appstoretest.Notification{NotificationType: "DID_FAIL_TO_RENEW", Subtype: "GRACE_PERIOD", NotificationUUID: "rollover-grace-event",
		SignedDate: h.clock.Now().UnixMilli(), Version: "2.0", Data: appstoretest.NotificationData{BundleID: h.cfg.IAP.AppleBundleID,
			Environment: "Production", SignedTransactionInfo: signed, SignedRenewalInfo: h.apple.Sign(t, renewal)}}
	mustStatus(t, h.do(http.MethodPost, "/api/v1/iap/apple/notifications", map[string]any{"signedPayload": h.apple.Sign(t, event)}, nil), http.StatusOK, "")
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	if usage.num("daily_credit_grant") != 40 || usage.num("purchased_credits") != 80 {
		t.Fatalf("verified grace lost daily access: %s", usage.raw)
	}
	// A same-time transaction replay carries the original paid end, while the
	// verified renewal event carries the grace extension. Keep that extension.
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	usage = h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	if usage.num("daily_credit_grant") != 40 {
		t.Fatalf("transaction replay shortened grace: %s", usage.raw)
	}
}
