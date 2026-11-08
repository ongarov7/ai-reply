package apptest

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/iap/appstore/appstoretest"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

func TestAppleBaseAdversarialUpgradeReplayKeepsEarnedCredits(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	// Use local midnight so the upgrade boundary has no partially paid day.
	h.clock.Advance(10 * time.Hour)
	h.enableApprovedProducts()
	s := h.signIn("base-upgrade-replay@example.com")
	start := h.clock.Now()
	end := start.Add(30 * 24 * time.Hour)
	base := h.subscriptionTx(s.userID, "upgrade-base", "upgrade-chain", "base_monthly", start, end, nil)
	mustStatus(t, h.postApple(s.access, base), http.StatusOK, "")
	for i := 0; i < 3; i++ {
		mustStatus(t, h.generate(s.access, "Сәлем"), http.StatusOK, "")
	}
	h.clock.Advance(24 * time.Hour)
	s = h.refresh(s)
	if got := h.creditsLeft(s.access); got != 77 {
		t.Fatalf("two paid days minus three replies: got %v, want 77", got)
	}
	h.clock.Advance(24 * time.Hour)
	pro := h.subscriptionTx(s.userID, "upgrade-pro", "upgrade-chain", "pro_monthly", h.clock.Now(), end, nil)
	mustStatus(t, h.appleNotification("DID_RENEW", "upgrade-pro-notice", pro), http.StatusOK, "")
	s = h.refresh(s)
	mustStatus(t, h.postApple(s.access, base), http.StatusOK, "")
	h.clock.Advance(5 * 24 * time.Hour)
	s = h.refresh(s)
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	mustStatus(t, usage, http.StatusOK, "")
	if usage.num("purchased_credits") != 77 || usage.num("daily_credit_grant") != 0 || usage.body["unlimited"] != true {
		t.Fatalf("upgrade or old Base replay changed earned credits or resumed accrual: %s", usage.raw)
	}
}

func TestAppleBaseAdversarialAccumulatedRepliesHaveNoDailySpendCap(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.clock.Advance(10 * time.Hour)
	h.enableApprovedProducts()
	s := h.signIn("base-spend-accumulated@example.com")
	start := h.clock.Now()
	signed := h.subscriptionTx(s.userID, "spend-base", "spend-chain", "base_monthly", start, start.Add(72*time.Hour), nil)
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	h.clock.Advance(24 * time.Hour)
	s = h.refresh(s)
	if got := h.creditsLeft(s.access); got != 80 {
		t.Fatalf("two paid days should accumulate 80 replies: got %v", got)
	}
	for i := 0; i < 41; i++ {
		result := h.generate(s.access, "Сәлем")
		mustStatus(t, result, http.StatusOK, "")
		if result.str("usage", "bucket") != "pack" || result.num("usage", "daily_limit") != 0 {
			t.Fatalf("reply %d did not spend from the permanent balance: %s", i+1, result.raw)
		}
	}
	if got := h.creditsLeft(s.access); got != 39 {
		t.Fatalf("41 replies on the same date must leave 39 accumulated replies: got %v", got)
	}
}

func TestAppleBaseAdversarialLateUpgradedHistoryRecoversPaidDays(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.clock.Advance(10 * time.Hour)
	h.enableApprovedProducts()
	s := h.signIn("base-late-upgraded-history@example.com")
	start := h.clock.Now()
	end := start.Add(30 * 24 * time.Hour)
	base := h.subscriptionTx(s.userID, "late-base", "late-chain", "base_monthly", start, end, nil)
	mustStatus(t, h.postApple(s.access, base), http.StatusOK, "")
	// Model a pre-rollover database whose latest retained subscription is Pro:
	// the Base purchase survives, but its old per-period expiry was never stored.
	h.exec(`DELETE FROM credit_grants WHERE user_id=? AND product_code='apple_base_daily'`, s.userID)
	h.exec(`DELETE FROM apple_base_credit_periods WHERE user_id=?`, s.userID)
	h.clock.Advance(48 * time.Hour)
	pro := h.subscriptionTx(s.userID, "late-pro", "late-chain", "pro_monthly", h.clock.Now(), end, nil)
	mustStatus(t, h.appleNotification("DID_RENEW", "late-pro-notice", pro), http.StatusOK, "")
	h.clock.Advance(3 * 24 * time.Hour)
	s = h.refresh(s)
	upgraded := h.subscriptionTx(s.userID, "late-base", "late-chain", "base_monthly", start, end, func(tx *appstoretest.Transaction) {
		tx.IsUpgraded = true
	})
	mustStatus(t, h.postApple(s.access, upgraded), http.StatusOK, "")
	if got := h.creditsLeft(s.access); got != 80 {
		t.Fatalf("late signed upgraded history must recover two paid Base days only: got %v, want 80", got)
	}
	h.clock.Advance(24 * time.Hour)
	s = h.refresh(s)
	mustStatus(t, h.postApple(s.access, base), http.StatusOK, "")
	if got := h.creditsLeft(s.access); got != 80 {
		t.Fatalf("old full-period replay reopened Base accrual: got %v, want 80", got)
	}
}

func TestAppleBaseAdversarialLegacyRetentionDoesNotRegrantSpentReplies(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.clock.Advance(10 * time.Hour)
	h.enableApprovedProducts()
	s := h.signIn("base-purged-reservations@example.com")
	start := h.clock.Now()
	signed := h.subscriptionTx(s.userID, "purged-base", "purged-chain", "base_monthly", start, start.Add(48*time.Hour), nil)
	user, err := h.store.UserByID(context.Background(), s.userID)
	if err != nil {
		t.Fatal(err)
	}
	// Verification records the paid interval without the HTTP handler reading
	// entitlement. Reserve directly to model charges made by the previous server.
	if _, err = h.iap.VerifyApple(context.Background(), user, signed); err != nil {
		t.Fatal(err)
	}
	date, month := h.todayKeys()
	for i := 0; i < 3; i++ {
		reservation, err := h.store.ReserveGeneration(context.Background(), repository.ReserveInput{
			UserID: s.userID, Date: date, Month: month, DailyLimit: 40, Mode: "reply", Now: start,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = h.store.CommitGeneration(context.Background(), reservation.ID, start); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := h.store.DeleteOldUsageReservations(context.Background(), start.Add(time.Minute), 100)
	if err != nil || removed != 3 {
		t.Fatalf("retention fixture: removed=%d, error=%v", removed, err)
	}
	h.clock.Advance(48 * time.Hour)
	s = h.refresh(s)
	if got := h.creditsLeft(s.access); got != 77 {
		t.Fatalf("retained usage counters must prevent regrant after request retention: got %v, want 77", got)
	}
}

func TestAppleBaseAdversarialUpgradedHistoryWaitsForVerifiedReplacement(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.clock.Advance(10 * time.Hour)
	h.enableApprovedProducts()
	s := h.signIn("base-upgraded-unknown-boundary@example.com")
	start := h.clock.Now()
	end := start.Add(30 * 24 * time.Hour)
	// isUpgraded identifies an ended Base interval, but its original expiry
	// cannot establish the actual upgrade time without replacement history.
	upgraded := h.subscriptionTx(s.userID, "unknown-base", "unknown-chain", "base_monthly", start, end, func(tx *appstoretest.Transaction) {
		tx.IsUpgraded = true
	})
	mustStatus(t, h.postApple(s.access, upgraded), http.StatusOK, "")
	h.clock.Advance(5 * 24 * time.Hour)
	s = h.refresh(s)
	if got := h.creditsLeft(s.access); got != 0 {
		t.Fatalf("unknown upgrade boundary must not mint future paid days: got %v", got)
	}
	pro := h.subscriptionTx(s.userID, "unknown-pro", "unknown-chain", "pro_monthly", start.Add(48*time.Hour), end, nil)
	mustStatus(t, h.appleNotification("DID_RENEW", "unknown-pro-notice", pro), http.StatusOK, "")
	if got := h.creditsLeft(s.access); got != 80 {
		t.Fatalf("verified replacement must recover the exact two paid days: got %v, want 80", got)
	}
	h.clock.Advance(24 * time.Hour)
	s = h.refresh(s)
	if got := h.creditsLeft(s.access); got != 80 {
		t.Fatalf("replacement must keep accrual stopped: got %v, want 80", got)
	}
}

func TestAppleBaseAdversarialDelayedUpgradeCapsNewerSignedBase(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.clock.Advance(10 * time.Hour)
	h.enableApprovedProducts()
	s := h.signIn("base-newer-signed-than-upgrade@example.com")
	start := h.clock.Now()
	end := start.Add(30 * 24 * time.Hour)
	base := h.subscriptionTx(s.userID, "delayed-base", "delayed-chain", "base_monthly", start, end, nil)
	mustStatus(t, h.postApple(s.access, base), http.StatusOK, "")
	h.clock.Advance(48 * time.Hour)
	s = h.refresh(s)
	// Apple can re-sign an old transaction after a newer purchase was signed.
	freshBase := h.subscriptionTx(s.userID, "delayed-base", "delayed-chain", "base_monthly", start, end, nil)
	mustStatus(t, h.postApple(s.access, freshBase), http.StatusOK, "")
	if got := h.creditsLeft(s.access); got != 120 {
		t.Fatalf("fixture should already contain three daily grants: got %v", got)
	}
	// A verified Pro notification can be delivered late with its earlier signed
	// timestamp. Its purchased_at still establishes the replacement boundary.
	upgrade := start.Add(24 * time.Hour)
	_, err := h.store.ApplyAppleSubscription(context.Background(), repository.AppleSubscription{
		UserID: s.userID, OriginalTransactionID: "delayed-chain", TransactionID: "delayed-pro",
		ProductID: "test.pro_monthly", PlanID: h.text(`SELECT id FROM plans WHERE code='pro_monthly'`),
		Environment: "Production", PurchasedAt: upgrade, ExpiresAt: end, SignedAt: upgrade,
	}, h.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	h.clock.Advance(24 * time.Hour)
	s = h.refresh(s)
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	mustStatus(t, usage, http.StatusOK, "")
	if usage.num("purchased_credits") != 120 || usage.num("daily_credit_grant") != 0 || usage.body["unlimited"] != true {
		t.Fatalf("earlier-signed upgrade failed to select Pro and stop later Base accrual: %s", usage.raw)
	}
}

func TestAppleBaseAdversarialOverlappingRefundsRestoreOnlyRemaining(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.clock.Advance(10 * time.Hour)
	h.enableApprovedProducts()
	s := h.signIn("base-overlap-refund@example.com")
	start := h.clock.Now()
	end := start.Add(8 * time.Hour)
	first := h.subscriptionTx(s.userID, "overlap-first", "overlap-chain", "base_monthly", start, end, nil)
	mustStatus(t, h.postApple(s.access, first), http.StatusOK, "")
	for i := 0; i < 3; i++ {
		mustStatus(t, h.generate(s.access, "Сәлем"), http.StatusOK, "")
	}
	h.clock.Advance(time.Hour)
	s = h.refresh(s)
	secondStart := h.clock.Now()
	second := h.subscriptionTx(s.userID, "overlap-second", "overlap-chain", "base_monthly", secondStart, end, nil)
	mustStatus(t, h.postApple(s.access, second), http.StatusOK, "")
	refund := func(id, notice string, purchased time.Time) {
		h.clock.Advance(time.Minute)
		tx := h.subscriptionTx(s.userID, id, "overlap-chain", "base_monthly", purchased, end, func(tx *appstoretest.Transaction) {
			tx.RevocationDate = h.clock.Now().UnixMilli()
		})
		mustStatus(t, h.appleNotification("REFUND", notice, tx), http.StatusOK, "")
	}
	refund("overlap-first", "overlap-refund-first", start)
	if got := h.creditsLeft(s.access); got != 37 {
		t.Fatalf("other paid coverage on the same day must preserve remaining credits: got %v", got)
	}
	refund("overlap-second", "overlap-refund-second", secondStart)
	if got := h.creditsLeft(s.access); got != 0 {
		t.Fatalf("refunding all paid coverage must remove remaining credits: got %v", got)
	}
	h.clock.Advance(time.Minute)
	reversal := h.subscriptionTx(s.userID, "overlap-first", "overlap-chain", "base_monthly", start, end, nil)
	mustStatus(t, h.appleNotification("REFUND_REVERSED", "overlap-reversed-first", reversal), http.StatusOK, "")
	mustStatus(t, h.appleNotification("REFUND_REVERSED", "overlap-reversed-first", reversal), http.StatusOK, "")
	if got := h.creditsLeft(s.access); got != 37 {
		t.Fatalf("reversal must restore only unspent credits once: got %v, want 37", got)
	}
}
