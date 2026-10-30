package apptest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/iap"
	"github.com/aireply/ai-reply-back-end/internal/iap/appstore"
	"github.com/aireply/ai-reply-back-end/internal/iap/appstore/appstoretest"
)

func approvedEntitlements() harnessOption { return withEnv("TEST_APPROVED_ENTITLEMENTS", "true") }

func (h *harness) enableApprovedProducts() {
	h.t.Helper()
	h.exec(`UPDATE iap_products SET apple_product_id='test.'||code,is_active=1 WHERE code IN ('pack30','base_monthly','base_yearly','pro_monthly','pro_yearly')`)
	mustStatus(h.t, h.adminCall(h.signInAdmin(), http.MethodPost, "/settings/iap", map[string]any{"enabled": true}), http.StatusOK, "")
}

func (h *harness) subscriptionTx(userID, transactionID, originalID, code string, at, expires time.Time, edit func(*appstoretest.Transaction)) string {
	h.t.Helper()
	return h.appleTx(userID, transactionID, "test."+code, func(tx *appstoretest.Transaction) {
		tx.Type = appstore.TypeAutoRenewable
		tx.OriginalTransactionID = originalID
		tx.PurchaseDate = at.UnixMilli()
		tx.ExpiresDate = expires.UnixMilli()
		tx.SignedDate = h.clock.Now().UnixMilli()
		if edit != nil {
			edit(tx)
		}
	})
}

func TestApprovedFreeIsTenLifetimeAndDoesNotRenew(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withWelcomeBonus(10))
	s := h.signIn("free-lifetime@example.com")
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	if usage.num("daily_limit") != 0 || usage.num("welcome", "total") != 10 || usage.num("remaining_total") != 10 {
		t.Fatalf("new free: %s", usage.raw)
	}
	for i := 0; i < 3; i++ {
		mustStatus(t, h.generate(s.access, "Жауап керек"), http.StatusOK, "")
	}
	h.clock.Advance(24 * time.Hour)
	s = h.refresh(s)
	usage = h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	if usage.num("welcome", "remaining") != 7 || usage.num("remaining_total") != 7 {
		t.Fatalf("daily reset renewed free: %s", usage.raw)
	}
	for i := 0; i < 7; i++ {
		mustStatus(t, h.generate(s.access, "Жауап керек"), http.StatusOK, "")
	}
	mustStatus(t, h.generate(s.access, "Артық"), http.StatusTooManyRequests, "DAILY_LIMIT_REACHED")
	h.clock.Advance(24 * time.Hour)
	s = h.refresh(s)
	mustStatus(t, h.generate(s.access, "Келесі күні"), http.StatusTooManyRequests, "DAILY_LIMIT_REACHED")
	again, _ := h.signInOn("free-lifetime@example.com", "ios")
	if h.welcomeLeft(again.userID) != 0 || h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE user_id=? AND kind='welcome'`, s.userID) != 1 {
		t.Fatal("sign-in regranted allowance")
	}
}

func TestApprovedPackIsPermanentAndNeedsNoSubscription(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableApprovedProducts()
	s := h.signIn("pack-no-subscription@example.com")
	for i := 0; i < 2; i++ {
		signed := h.appleTx(s.userID, fmt.Sprintf("pack30-%d", i), "test.pack30", func(tx *appstoretest.Transaction) { tx.Price = 100000; tx.Currency = "KZT" })
		mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
		mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	}
	if h.creditsLeft(s.access) != 60 {
		t.Fatal("packs not accumulated exactly once")
	}
	first := h.generate(s.access, "Сәлем")
	mustStatus(t, first, http.StatusOK, "")
	if first.str("usage", "bucket") != "pack" || first.num("usage", "purchased_credits") != 59 {
		t.Fatalf("pack balance: %s", first.raw)
	}
	h.clock.Advance(400 * 24 * time.Hour)
	// A fresh login models another installation after the old refresh session expired.
	s, _ = h.signInOn("pack-no-subscription@example.com", "ios")
	if h.creditsLeft(s.access) != 59 {
		t.Fatal("permanent credits expired")
	}
}

func TestAppleBaseRenewsFromSignedExpiryAndIgnoresOldPeriod(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableApprovedProducts()
	s := h.signIn("base-renew@example.com")
	start := h.clock.Now()
	end := start.Add(30 * 24 * time.Hour)
	first := h.subscriptionTx(s.userID, "base-1", "base-original", "base_monthly", start, end, nil)
	mustStatus(t, h.postApple(s.access, first), http.StatusOK, "")
	for i := 0; i < 30; i++ {
		mustStatus(t, h.generate(s.access, "Сәлем"), http.StatusOK, "")
	}
	mustStatus(t, h.generate(s.access, "Артық"), http.StatusTooManyRequests, "DAILY_LIMIT_REACHED")
	h.clock.Advance(24 * time.Hour)
	s = h.refresh(s)
	mustStatus(t, h.generate(s.access, "Ертең"), http.StatusOK, "")
	// Renewal arrives from Apple without the app running.
	renewed := h.subscriptionTx(s.userID, "base-2", "base-original", "base_monthly", end, end.Add(30*24*time.Hour), nil)
	mustStatus(t, h.appleNotification("DID_RENEW", "base-renew-notice", renewed), http.StatusOK, "")
	// Reposting the first paid period must not shorten the new entitlement.
	mustStatus(t, h.postApple(s.access, first), http.StatusOK, "")
	me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access))
	if me.str("subscription", "plan", "code") != "base_monthly" || me.str("subscription", "expires_at") != end.Add(30*24*time.Hour).Format(time.RFC3339) {
		t.Fatalf("renewal: %s", me.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE user_id=?`, s.userID); n != 2 {
		t.Fatalf("history count %d", n)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE user_id=? AND kind='pack'`, s.userID); n != 0 {
		t.Fatal("subscription created package credits")
	}
}

func TestAppleProNeverConsumesCreditsAndFailedCallsRefundAnalytics(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t), withWelcomeBonus(10))
	h.enableApprovedProducts()
	s := h.signIn("pro-unlimited@example.com")
	start := h.clock.Now()
	h.addGrant(s.userID, domain.GrantPack, 60, start, nil, start)
	signed := h.subscriptionTx(s.userID, "pro-1", "pro-original", "pro_monthly", start, start.Add(30*24*time.Hour), nil)
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	for i := 0; i < 40; i++ {
		res := h.generate(s.access, "Сәлем")
		mustStatus(t, res, http.StatusOK, "")
		if res.str("usage", "bucket") != "unlimited" || res.body["usage"].(map[string]any)["unlimited"] != true {
			t.Fatalf("unlimited state: %s", res.raw)
		}
	}
	first := h.generateWithKey(s.access, "Сәлем", "pro-idempotency")
	mustStatus(t, first, http.StatusOK, "")
	mustStatus(t, h.generateWithKey(s.access, "Сәлем", "pro-idempotency"), http.StatusOK, "")
	h.provider.err = domain.ErrProviderDown
	mustStatus(t, h.generate(s.access, "Сәлем"), http.StatusBadGateway, "AI_PROVIDER_UNAVAILABLE")
	h.provider.err = nil
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	if usage.num("purchased_credits") != 60 || usage.num("welcome", "remaining") != 10 || usage.num("used_today") != 41 {
		t.Fatalf("pro consumed balances or failed to refund: %s", usage.raw)
	}
}

func TestAppleSubscriptionRefundExpiryAndOwnership(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableApprovedProducts()
	s := h.signIn("sub-lifecycle@example.com")
	other := h.signIn("sub-other@example.com")
	start := h.clock.Now()
	end := start.Add(time.Hour)
	signed := h.subscriptionTx(s.userID, "life-1", "life-original", "pro_monthly", start, end, nil)
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	mustStatus(t, h.postApple(other.access, signed), http.StatusBadRequest, "PURCHASE_INVALID")
	h.addGrant(s.userID, domain.GrantPack, 30, start, nil, start)
	revoked := h.subscriptionTx(s.userID, "life-1", "life-original", "pro_monthly", start, end, func(tx *appstoretest.Transaction) {
		tx.RevocationDate = start.Add(time.Minute).UnixMilli()
		tx.SignedDate = start.Add(time.Minute).UnixMilli()
	})
	mustStatus(t, h.appleNotification("REFUND", "life-refund", revoked), http.StatusOK, "")
	mustStatus(t, h.appleNotification("REFUND", "life-refund", revoked), http.StatusOK, "")
	// Old paid JWS cannot resurrect a refunded subscription.
	mustStatus(t, h.postApple(s.access, signed), http.StatusBadRequest, "PURCHASE_INVALID")
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	if usage.body["unlimited"] != false || usage.num("purchased_credits") != 30 {
		t.Fatalf("refund: %s", usage.raw)
	}
	// Explicit signed REFUND_REVERSED is allowed to restore the remaining period.
	h.clock.Advance(2 * time.Minute)
	reversed := h.subscriptionTx(s.userID, "life-1", "life-original", "pro_monthly", start, end, nil)
	mustStatus(t, h.appleNotification("REFUND_REVERSED", "life-reversed", reversed), http.StatusOK, "")
	usage = h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	if usage.body["unlimited"] != true {
		t.Fatalf("refund reversal: %s", usage.raw)
	}
	h.clock.Advance(2 * time.Hour)
	s = h.refresh(s)
	usage = h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	if usage.body["unlimited"] != false || usage.num("purchased_credits") != 30 {
		t.Fatalf("expiration: %s", usage.raw)
	}
}

func TestIAPAccountTokenSurvivesSigningSecretRotation(t *testing.T) {
	h := newHarness(t, withIAP(t))
	s := h.signIn("token-persist@example.com")
	first := h.iap.AccountToken(s.userID)
	rotated, err := iap.New(iap.Deps{Repo: h.store, Config: h.cfg.IAP, Secret: strings.Repeat("new-secret-", 8)})
	if err != nil {
		t.Fatal(err)
	}
	if got := rotated.AccountToken(s.userID); got != first {
		t.Fatal("account token changed with signing key")
	}
	if _, err := h.store.UserByIAPAccountToken(context.Background(), first); err != nil {
		t.Fatal(err)
	}
}

func TestPackageRecommendationUsesVerifiedSpendingAndStopsForSubscriber(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableApprovedProducts()
	s := h.signIn("recommendation@example.com")
	for i := 0; i < 4; i++ {
		signed := h.appleTx(s.userID, fmt.Sprintf("recommend-%d", i), "test.pack30", func(tx *appstoretest.Transaction) { tx.Price = 100000; tx.Currency = "KZT" })
		mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	}
	catalogue := h.products(s.access, "ios")
	if catalogue.num("recommendation", "purchase_count") != 4 || catalogue.num("recommendation", "spent_minor") != 40000 || catalogue.num("recommendation", "base_price_minor") != 59900 {
		t.Fatalf("recommendation: %s", catalogue.raw)
	}
	start := h.clock.Now()
	mustStatus(t, h.postApple(s.access, h.subscriptionTx(s.userID, "recommend-base", "recommend-original", "base_monthly", start, start.Add(30*24*time.Hour), nil)), http.StatusOK, "")
	if rec := h.products(s.access, "ios").body["recommendation"]; rec != nil {
		t.Fatalf("subscriber recommendation: %v", rec)
	}
}

// Keep an explicit assertion that invalid signed data never earns an entitlement.
func TestSubscriptionMalformedExpiryCannotGrant(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableApprovedProducts()
	s := h.signIn("sub-bad@example.com")
	tx := h.subscriptionTx(s.userID, "bad-expiry", "bad-original", "pro_monthly", h.clock.Now(), time.UnixMilli(0), nil)
	mustStatus(t, h.postApple(s.access, tx), http.StatusBadRequest, "PURCHASE_INVALID")
	_, err := h.store.ActiveAppleSubscription(context.Background(), s.userID, h.clock.Now())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("bad subscription active: %v", err)
	}
}

func TestAppleCancellationAndGraceUseSignedRenewalState(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableApprovedProducts()
	s := h.signIn("renewal-state@example.com")
	start := h.clock.Now()
	end := start.Add(time.Hour)
	tx := h.subscriptionTx(s.userID, "renewal-state-1", "renewal-state-original", "base_monthly", start, end, nil)
	mustStatus(t, h.postApple(s.access, tx), http.StatusOK, "")
	notice := func(kind, subtype, id string, auto int, grace time.Time) {
		renewal := appstore.Renewal{OriginalTransactionID: "renewal-state-original", AutoRenewStatus: auto, Environment: "Production", SignedDate: h.clock.Now().UnixMilli()}
		if !grace.IsZero() {
			renewal.GracePeriodExpiresDate = grace.UnixMilli()
		}
		payload := appstoretest.Notification{NotificationType: kind, Subtype: subtype, NotificationUUID: id, Version: "2.0", SignedDate: h.clock.Now().UnixMilli(), Data: appstoretest.NotificationData{BundleID: h.cfg.IAP.AppleBundleID, Environment: "Production", SignedTransactionInfo: tx, SignedRenewalInfo: h.apple.Sign(t, renewal)}}
		mustStatus(t, h.do(http.MethodPost, "/api/v1/iap/apple/notifications", map[string]any{"signedPayload": h.apple.Sign(t, payload)}, nil), http.StatusOK, "")
	}
	h.clock.Advance(time.Minute)
	notice("DID_CHANGE_RENEWAL_STATUS", "AUTO_RENEW_DISABLED", "renewal-canceled", 0, time.Time{})
	me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access))
	if me.str("subscription", "plan", "code") != "base_monthly" || me.body["subscription"].(map[string]any)["auto_renews"] != false {
		t.Fatalf("cancellation prematurely expired: %s", me.raw)
	}
	// A signed grace period is an entitlement; an unsigned client date is never used.
	h.clock.Advance(time.Minute)
	notice("DID_FAIL_TO_RENEW", "GRACE_PERIOD", "renewal-grace", 1, end.Add(2*time.Hour))
	h.clock.Advance(90 * time.Minute)
	s = h.refresh(s)
	me = h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access))
	if me.str("subscription", "plan", "code") != "base_monthly" {
		t.Fatalf("grace missing: %s", me.raw)
	}
	notice("GRACE_PERIOD_EXPIRED", "", "renewal-grace-expired", 1, end.Add(2*time.Hour))
	me = h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access))
	if me.str("subscription", "plan", "code") != "free" {
		t.Fatalf("grace did not expire: %s", me.raw)
	}
}
