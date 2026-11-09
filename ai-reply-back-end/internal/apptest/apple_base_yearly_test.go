package apptest

import (
	"net/http"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/iap/appstore"
	"github.com/aireply/ai-reply-back-end/internal/iap/appstore/appstoretest"
)

// Base Yearly is named separately in Apple's rejection. It retains the sold
// daily contract even when new annual sales are disabled by migration 0037.
func TestAppleYearlyBaseBalanceSurvivesCancellationExpiryAndHiddenProductRestore(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableApprovedProducts()
	s := h.signIn("yearly-permanent@example.com")
	local := h.clock.Now().In(h.cfg.App.Location())
	start := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, h.cfg.App.Location())
	h.clock.Advance(start.Sub(h.clock.Now()))
	s = h.refresh(s)
	end := start.AddDate(1, 0, 0)
	signed := h.subscriptionTx(s.userID, "yearly-permanent", "yearly-permanent-chain", "base_yearly", start, end, nil)
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	for i := 0; i < 7; i++ {
		mustStatus(t, h.generate(s.access, "Жылдық жауап"), http.StatusOK, "")
	}
	if left := h.creditsLeft(s.access); left != 33 {
		t.Fatalf("initial yearly remainder: %v", left)
	}

	// Removing a product from sale must leave its verified purchases usable.
	h.exec(`UPDATE iap_products SET is_active=0 WHERE code='base_yearly'`)
	h.exec(`UPDATE plans SET is_visible=0 WHERE code='base_yearly'`)
	h.clock.Advance(time.Minute)
	renewal := appstore.Renewal{OriginalTransactionID: "yearly-permanent-chain", Environment: "Production", AutoRenewStatus: 0, SignedDate: h.clock.Now().UnixMilli()}
	event := appstoretest.Notification{NotificationType: "DID_CHANGE_RENEWAL_STATUS", NotificationUUID: "yearly-cancel", SignedDate: h.clock.Now().UnixMilli(), Version: "2.0", Data: appstoretest.NotificationData{BundleID: h.cfg.IAP.AppleBundleID, Environment: "Production", SignedTransactionInfo: signed, SignedRenewalInfo: h.apple.Sign(t, renewal)}}
	mustStatus(t, h.do(http.MethodPost, "/api/v1/iap/apple/notifications", map[string]any{"signedPayload": h.apple.Sign(t, event)}, nil), http.StatusOK, "")
	if left := h.creditsLeft(s.access); left != 33 {
		t.Fatalf("cancellation removed yearly remainder: %v", left)
	}

	h.clock.Advance(end.AddDate(1, 0, 0).Sub(h.clock.Now()))
	s, _ = h.signInOn("yearly-permanent@example.com", "android")
	paidDays := int(end.Sub(start) / (24 * time.Hour))
	want := paidDays*40 - 7
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	mustStatus(t, usage, http.StatusOK, "")
	if usage.num("daily_limit") != 0 || usage.num("daily_credit_grant") != 0 || usage.num("purchased_credits") != float64(want) || usage.body["credits_never_expire"] != true {
		t.Fatalf("expired yearly balance: %s; want %d", usage.raw, want)
	}
	mustStatus(t, h.generate(s.access, "Жазылымнан кейін"), http.StatusOK, "")
	if left := h.creditsLeft(s.access); left != float64(want-1) {
		t.Fatalf("expired yearly replies cannot be spent: %v", left)
	}

	h.clock.Advance(time.Minute)
	s = h.signIn("yearly-permanent@example.com")
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	if left := h.creditsLeft(s.access); left != float64(want-1) {
		t.Fatalf("hidden yearly product restore changed balance: %v", left)
	}
	if days := h.scalar(`SELECT COUNT(*) FROM apple_base_credit_days WHERE user_id=?`, s.userID); days != paidDays {
		t.Fatalf("yearly accrual included unpaid days: %d, want %d", days, paidDays)
	}
	if expiring := h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE user_id=? AND expires_at IS NOT NULL`, s.userID); expiring != 0 {
		t.Fatal("yearly credits have an expiration date")
	}
	if ledger := h.scalar(`SELECT SUM(delta) FROM credit_ledger WHERE user_id=?`, s.userID); ledger != want-1 {
		t.Fatalf("yearly ledger: %d, want %d", ledger, want-1)
	}
}
