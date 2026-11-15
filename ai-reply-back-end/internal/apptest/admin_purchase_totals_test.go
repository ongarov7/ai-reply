package apptest

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// Pack buyers need to appear in purchase reporting even though they have no
// paid subscription. Retrying a verified transaction must not increase sales.
func TestAdminDashboardCountsVerifiedPackPurchasesAndReplay(t *testing.T) {
	h := newHarness(t, withIAP(t))
	admin := h.openIAP()
	buyer := h.signIn("purchase-dashboard@example.com")
	check := func(purchases, buyers float64) {
		t.Helper()
		res := h.adminCall(admin, http.MethodGet, "/dashboard?range=30d", nil)
		mustStatus(t, res, http.StatusOK, "")
		if res.num("stats", "purchases_range") != purchases || res.num("stats", "buyers_range") != buyers || res.num("stats", "paid_users") != 0 {
			t.Fatalf("pack purchase totals: %s", res.raw)
		}
	}
	check(0, 0)
	signed := h.appleTx(buyer.userID, "dashboard-pack-first", packApple, nil)
	mustStatus(t, h.postApple(buyer.access, signed), http.StatusOK, "")
	check(1, 1)
	mustStatus(t, h.postApple(buyer.access, signed), http.StatusOK, "")
	check(1, 1)
	mustStatus(t, h.postApple(buyer.access, h.appleTx(buyer.userID, "dashboard-pack-second", packApple, nil)), http.StatusOK, "")
	check(2, 1)
}

func TestAdminPurchaseTotalsIncludeOnlyCompletedProductionPurchasesInRange(t *testing.T) {
	h := newHarness(t)
	first := h.signIn("purchase-totals-first@example.com")
	second := h.signIn("purchase-totals-second@example.com")
	to := h.clock.Now()
	from := to.Add(-24 * time.Hour)
	middle := from.Add(12 * time.Hour)
	iap := func(id, provider, environment, status string, userID any, purchased time.Time) {
		t.Helper()
		// created_at deliberately differs from purchased_at: restoring an older
		// transaction must count in its actual purchase period.
		h.exec(`INSERT INTO iap_purchases(id,user_id,provider,environment,product_code,transaction_id,status,purchased_at,created_at,updated_at)
			VALUES(?,?,?,?,?,?, ?,?,?,?)`, id, userID, provider, environment, "pack30", id, status, purchased.UnixMilli(), to.AddDate(0, 1, 0).UnixMilli(), to.UnixMilli())
	}
	iap("production-start", "app_store", "Production", "granted", first.userID, from)
	iap("production-end", "google_play", "Production", "granted", first.userID, to)
	iap("production-renewal", "app_store", "Production", "granted", first.userID, middle)
	// Account deletion anonymizes financial records; it does not erase a sale.
	iap("deleted-buyer", "app_store", "Production", "granted", nil, middle)
	for _, environment := range []string{"Sandbox", "Test", "Promo"} {
		iap("excluded-"+environment, "app_store", environment, "granted", second.userID, middle)
	}
	for _, status := range []string{"pending", "refunded", "revoked", "rejected"} {
		iap("excluded-"+status, "google_play", "Production", status, second.userID, middle)
	}
	iap("before", "app_store", "Production", "granted", second.userID, from.Add(-time.Millisecond))
	iap("after", "app_store", "Production", "granted", second.userID, to.Add(time.Millisecond))

	if h.scalar(`SELECT COUNT(*) FROM billing_catalog_versions WHERE state='published'`) == 0 {
		h.exec(`INSERT INTO billing_catalog_versions(state,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at)
			VALUES('published','[]','test','test','test',?,?,?)`, from.UnixMilli(), from.UnixMilli(), from.UnixMilli())
	}
	bank := func(id, environment, status, userID string, created time.Time) {
		t.Helper()
		h.exec(`INSERT INTO billing_orders(id,user_id,idempotency_key,catalog_version,product_code,snapshot_json,amount_minor,currency,provider,environment,bank_order,status,checkout_token_hash,request_json,created_at,updated_at)
			SELECT ?,?,?,id,'pack30','{}',13000,'KZT','bcc',?,?,?,'hash','{}',?,? FROM billing_catalog_versions WHERE state='published'`,
			id, userID, id, environment, id, status, created.UnixMilli(), to.UnixMilli())
	}
	bank("bank-production", "production", "succeeded", second.userID, middle)
	bank("bank-existing-buyer", "production", "succeeded", first.userID, middle)
	bank("bank-sandbox", "sandbox", "succeeded", second.userID, middle)
	bank("bank-refunded", "production", "refunded", second.userID, middle)
	bank("bank-pending", "production", "pending", second.userID, middle)
	bank("bank-before", "production", "succeeded", second.userID, from.Add(-time.Millisecond))
	bank("bank-after", "production", "succeeded", second.userID, to.Add(time.Millisecond))
	h.exec(`INSERT INTO payments(id,user_id,plan_id,provider,amount,currency,status,created_at,updated_at)
		SELECT 'legacy-demo',?,id,'demo',399000,'KZT','succeeded',?,? FROM plans WHERE code='free'`, second.userID, middle.UnixMilli(), middle.UnixMilli())

	totals, err := h.store.PurchaseTotals(context.Background(), from, to)
	if err != nil {
		t.Fatal(err)
	}
	if totals.Purchases != 6 || totals.Buyers != 2 {
		t.Fatalf("totals = %+v, want 6 transactions and 2 known buyers", totals)
	}
	empty, err := h.store.PurchaseTotals(context.Background(), to.AddDate(0, 2, 0), to.AddDate(0, 3, 0))
	if err != nil || empty.Purchases != 0 || empty.Buyers != 0 {
		t.Fatalf("empty range = %+v, err = %v", empty, err)
	}
}
