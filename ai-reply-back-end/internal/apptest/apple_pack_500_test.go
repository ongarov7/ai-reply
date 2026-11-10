package apptest

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/catalog"
)

func replyPack500Harness(t *testing.T) *harness {
	h := newHarness(t, withEnv("TEST_APPROVED_ENTITLEMENTS", "true"), withIAP(t))
	h.exec(`UPDATE iap_products SET apple_product_id='test.'||code,is_active=1 WHERE code IN ('base_monthly','base_yearly','pro_monthly','pro_yearly','pack30')`)
	mustStatus(t, h.adminCall(h.signInAdmin(), http.MethodPost, "/settings/iap", map[string]any{"enabled": true}), http.StatusOK, "")
	return h
}

func TestAppleCatalogRetiresBaseAndOffers500PermanentReplies(t *testing.T) {
	h := replyPack500Harness(t)
	// Also exercise a later admin publication: without another store mapped,
	// the retired Base IAP row becomes inactive but remains a valid contract.
	prices := catalog.New(h.db)
	ctx := context.Background()
	if err := prices.EnsureInitial(ctx); err != nil {
		t.Fatal(err)
	}
	lineup := catalog.InitialProducts()
	for i := 1; i < 6; i++ {
		lineup[i].AppleProductID = "test." + lineup[i].Code
	}
	draft, err := prices.SaveDraft(ctx, "owner", 0, lineup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prices.Publish(ctx, "owner", draft.ID, draft.Revision); err != nil {
		t.Fatal(err)
	}
	if h.scalar(`SELECT is_active FROM iap_products WHERE code='base_monthly'`) != 0 {
		t.Fatal("fixture did not exercise inactive legacy Base")
	}
	s := h.signIn("apple-new-catalog@example.com")
	res := h.do(http.MethodGet, "/api/v1/iap/products?platform=ios", nil, h.auth(s.access))
	mustStatus(t, res, http.StatusOK, "")
	products := res.body["products"].([]any)
	seen := map[string]bool{}
	for _, item := range products {
		product := item.(map[string]any)
		code := product["code"].(string)
		seen[code] = true
		if code == "base_monthly" || code == "base_yearly" {
			t.Fatalf("retired subscription offered: %s", res.raw)
		}
		if code == "pack500" && (product["units"] != float64(500) || product["price"] != float64(99000) || product["currency"] != "KZT" || product["store_product_id"] != "kz.aireply.pack500" || product["credits_never_expire"] != true || product["kind"] != "pack") {
			t.Fatalf("wrong pack contract: %s", res.raw)
		}
	}
	for _, code := range []string{"pack500", "pack30", "pro_monthly", "pro_yearly"} {
		if !seen[code] {
			t.Fatalf("missing %s: %s", code, res.raw)
		}
	}
	// New-sale retirement must not reject a paid or renewed legacy Base period.
	h.exec(`UPDATE system_settings SET value='0' WHERE key='apple_base_monthly_since'`)
	start := h.clock.Now()
	paid := h.subscriptionTx(s.userID, "retired-base-paid", "retired-base-chain", "base_monthly", start, start.AddDate(0, 1, 0), nil)
	mustStatus(t, h.postApple(s.access, paid), http.StatusOK, "")
	if h.creditsLeft(s.access) != 1000 {
		t.Fatal("retiring new sales rejected an already paid Base period")
	}
	renewal := h.subscriptionTx(s.userID, "retired-base-renewal", "retired-base-chain", "base_monthly", start.AddDate(0, 1, 0), start.AddDate(0, 2, 0), nil)
	mustStatus(t, h.appleNotification("DID_RENEW", "retired-base-notification", renewal), http.StatusOK, "")
	h.clock.Advance(start.AddDate(0, 1, 0).Sub(h.clock.Now()))
	s, _ = h.signInOn("apple-new-catalog@example.com", "ios")
	if h.creditsLeft(s.access) != 2000 {
		t.Fatal("retiring new sales rejected an existing Base renewal")
	}
	// Android keeps its separately configured Base/pack30 lineup and never
	// advertises the new Apple-only SKU.
	h.exec(`UPDATE iap_products SET google_product_id='google.'||code,is_active=1,off_android=0 WHERE code IN ('base_monthly','pack30')`)
	android := h.do(http.MethodGet, "/api/v1/iap/products?platform=android", nil, h.auth(s.access))
	mustStatus(t, android, http.StatusOK, "")
	androidSeen := map[string]bool{}
	for _, item := range android.body["products"].([]any) {
		androidSeen[item.(map[string]any)["code"].(string)] = true
	}
	if !androidSeen["base_monthly"] || !androidSeen["pack30"] || androidSeen["pack500"] {
		t.Fatalf("Apple change altered Android offers: %s", android.raw)
	}
}

func TestApple500ReplyPackAccumulatesOnceWithoutExpiryAndKeepsOldPackContract(t *testing.T) {
	h := replyPack500Harness(t)
	s := h.signIn("apple-500-replies@example.com")
	other := h.signIn("apple-500-other@example.com")
	signed := h.appleTx(s.userID, "reply-pack-500", "kz.aireply.pack500", nil)
	mustStatus(t, h.postApple(other.access, signed), http.StatusBadRequest, "PURCHASE_INVALID")
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	second := h.appleTx(s.userID, "reply-pack-500-second", "kz.aireply.pack500", nil)
	mustStatus(t, h.postApple(s.access, second), http.StatusOK, "")
	old := h.appleTx(s.userID, "reply-pack-30-delayed", "test.pack30", nil)
	mustStatus(t, h.postApple(s.access, old), http.StatusOK, "")
	if h.creditsLeft(s.access) != 1030 || h.creditsLeft(other.access) != 0 {
		t.Fatal("purchase replay, binding or old 30-pack identity changed the balance")
	}
	if n := h.scalar(`SELECT count(*) FROM credit_grants WHERE user_id=? AND product_code='pack500' AND units_total=500 AND expires_at IS NULL`, s.userID); n != 2 {
		t.Fatalf("permanent 500 grants: %d", n)
	}
	if n := h.scalar(`SELECT count(*) FROM apple_subscriptions WHERE user_id=?`, s.userID); n != 0 {
		t.Fatal("consumable created a subscription")
	}
	h.clock.Advance(400 * 24 * time.Hour)
	s, _ = h.signInOn("apple-500-replies@example.com", "ios")
	if h.creditsLeft(s.access) != 1030 {
		t.Fatal("purchased replies expired")
	}
	mustStatus(t, h.generate(s.access, "Сәлем"), http.StatusOK, "")
	if h.creditsLeft(s.access) != 1029 {
		t.Fatal("old purchased replies are not spendable")
	}
}
