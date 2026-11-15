package apptest

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/iap/appstore/appstoretest"
)

func purchaseBreakdownByCode(t *testing.T, res response) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, row := range reportingRows(t, res, "purchase_breakdown") {
		code, ok := row["product_code"].(string)
		if !ok || code == "" || out[code] != nil {
			t.Fatalf("each product needs one distinct summary: %s", res.raw)
		}
		out[code] = row
	}
	return out
}

func purchaseAmountsByCurrency(t *testing.T, product map[string]any) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	amounts, ok := product["amounts"].([]any)
	if !ok {
		t.Fatalf("amounts must be an array, got %v", product)
	}
	for _, item := range amounts {
		amount := item.(map[string]any)
		currency := amount["currency"].(string)
		if _, exists := out[currency]; currency == "" || exists {
			t.Fatalf("currencies must be kept separately: %v", product)
		}
		out[currency] = int64(amount["amount_milliunits"].(float64))
	}
	return out
}

func TestAdminPurchaseBreakdownReflectsVerifiedPacksAndSubscription(t *testing.T) {
	h := replyPack500Harness(t)
	admin := h.signInAdmin()
	first := h.signIn("breakdown-first@example.com")
	second := h.signIn("breakdown-second@example.com")
	pack30 := h.appleTx(first.userID, "private-breakdown-transaction-30", "test.pack30", func(tx *appstoretest.Transaction) { tx.Price = 130000 })
	mustStatus(t, h.postApple(first.access, pack30), http.StatusOK, "")
	mustStatus(t, h.postApple(first.access, pack30), http.StatusOK, "")
	mustStatus(t, h.postApple(first.access, h.appleTx(first.userID, "private-breakdown-transaction-usd", "test.pack30", func(tx *appstoretest.Transaction) {
		tx.Price, tx.Currency, tx.Quantity = 3000, "USD", 2
	})), http.StatusOK, "")
	mustStatus(t, h.postApple(second.access, h.appleTx(second.userID, "private-breakdown-transaction-500", "kz.aireply.pack500", nil)), http.StatusOK, "")
	now := h.clock.Now()
	mustStatus(t, h.postApple(second.access, h.subscriptionTx(second.userID, "private-breakdown-transaction-pro", "private-breakdown-subscription-root", "pro_monthly", now, now.AddDate(0, 1, 0), func(tx *appstoretest.Transaction) {
		tx.Price = 3990000
	})), http.StatusOK, "")
	// A catalogue edit after the sale must never rewrite the paid amount.
	h.exec(`UPDATE iap_products SET price_minor=15900 WHERE code='pack30'`)

	dashboard := h.adminCall(admin, http.MethodGet, "/dashboard?range=30d", nil)
	mustStatus(t, dashboard, http.StatusOK, "")
	if dashboard.num("stats", "purchases_range") != 4 || dashboard.num("stats", "buyers_range") != 2 || dashboard.num("stats", "paid_users") != 1 {
		t.Fatalf("replays, pack buyers and subscriptions must be distinct: %s", dashboard.raw)
	}
	products := purchaseBreakdownByCode(t, dashboard)
	if len(products) != 3 {
		t.Fatalf("want both packs and the subscription separately: %s", dashboard.raw)
	}
	for _, tc := range []struct {
		code, kind   string
		count, units int
	}{{"pack30", "pack", 2, 90}, {"pack500", "pack", 1, 500}, {"pro_monthly", "subscription", 1, 0}} {
		row := products[tc.code]
		if row == nil || row["kind"] != tc.kind || row["purchases"] != float64(tc.count) || row["buyers"] != float64(1) || row["units"] != float64(tc.units) || row["name"] == "" {
			t.Errorf("summary %s = %v", tc.code, row)
		}
	}
	if products["pack30"]["catalog_price_minor"] != float64(15900) || products["pack30"]["catalog_currency"] != "KZT" {
		t.Fatalf("catalogue price must be identified separately: %v", products["pack30"])
	}
	amounts := purchaseAmountsByCurrency(t, products["pack30"])
	if len(amounts) != 2 || amounts["KZT"] != 130000 || amounts["USD"] != 3000 {
		t.Fatalf("signed money must retain each currency: %v", products["pack30"])
	}
	if got := purchaseAmountsByCurrency(t, products["pack500"]); got["KZT"] != 990000 {
		t.Fatalf("500 pack signed amount = %v", got)
	}
	if got := purchaseAmountsByCurrency(t, products["pro_monthly"]); got["KZT"] != 3990000 {
		t.Fatalf("subscription signed amount = %v", got)
	}

	seen := map[string]bool{}
	for page := 1; page <= 2; page++ {
		res := h.adminCall(admin, http.MethodGet, fmt.Sprintf("/dashboard/purchases?range=30d&product=pack30&limit=1&page=%d", page), nil)
		rows := reportingRows(t, res, "purchases")
		if res.num("total") != 2 || len(rows) != 1 || res.str("product") != "pack30" || res.num("page") != float64(page) {
			t.Fatalf("pack filter pagination: %s", res.raw)
		}
		row := rows[0]
		id, _ := row["id"].(string)
		quantity, _ := row["quantity"].(float64)
		if id == "" || seen[id] || row["product_code"] != "pack30" || row["user_id"] != first.userID || row["identifier"] == "" || row["price_known"] != true || (quantity != 1 && quantity != 2) || row["units"] != 30*quantity {
			t.Fatalf("buyer history must identify what this person bought: %s", res.raw)
		}
		seen[id] = true
		if strings.Contains(string(res.raw), "private-breakdown-") || strings.Contains(string(res.raw), "breakdown-first@example.com") {
			t.Fatalf("history exposed private transaction metadata or unmasked contact: %s", res.raw)
		}
	}
	unknown := h.adminCall(admin, http.MethodGet, "/dashboard/purchases?product="+url.QueryEscape("pack30' OR 1=1 --"), nil)
	mustStatus(t, unknown, http.StatusOK, "")
	if unknown.num("total") != 0 || len(reportingRows(t, unknown, "purchases")) != 0 {
		t.Fatalf("product filter must be literal: %s", unknown.raw)
	}
}

func TestAdminPurchaseBreakdownPreservesUnknownPricesBankSnapshotsAndDeletedSales(t *testing.T) {
	h := newHarness(t, withEnv("TEST_APPROVED_ENTITLEMENTS", "true"))
	admin := h.signInAdmin()
	first := h.signIn("breakdown-fixture@example.com")
	second := h.signIn("breakdown-bank@example.com")
	now := h.clock.Now()
	inside := now.Add(-time.Hour)
	iap := func(id, code, provider, environment, status string, user any, price int64, currency string, purchased time.Time) {
		t.Helper()
		h.exec(`INSERT INTO iap_purchases(id,user_id,provider,environment,store_product_id,product_code,transaction_id,original_transaction_id,purchase_token_sealed,quantity,units,status,purchased_at,created_at,updated_at,price_milliunits,currency)
			VALUES(?,?,?,?,?,?,?,'private-original-id','private-sealed-token',1,30,?,?,?,?,?,?)`,
			id, user, provider, environment, "retired.store.product", code, "private-tx-"+id, status, purchased.UnixMilli(), now.UnixMilli(), now.UnixMilli(), price, currency)
	}
	iap("unknown-play", "pack30", "google_play", "Production", "granted", first.userID, 0, "", inside)
	iap("deleted-apple", "retired_package", "app_store", "Production", "granted", nil, 85000, "KZT", inside)
	// Inactive, archived products still describe historical sales.
	h.exec(`UPDATE iap_products SET is_active=0,archived_at=?,price_minor=77700 WHERE code='pack30'`, now.UnixMilli())
	for _, tc := range []struct{ id, environment, status string }{
		{"sandbox", "Sandbox", "granted"}, {"test", "Test", "granted"}, {"promo", "Promo", "granted"},
		{"refunded", "Production", "refunded"}, {"revoked", "Production", "revoked"}, {"pending", "Production", "pending"},
	} {
		iap(tc.id, "pack30", "app_store", tc.environment, tc.status, first.userID, 130000, "KZT", inside)
	}
	iap("too-old", "pack30", "app_store", "Production", "granted", first.userID, 130000, "KZT", now.AddDate(0, 0, -40))
	iap("future", "pack30", "app_store", "Production", "granted", first.userID, 130000, "KZT", now.Add(time.Hour))
	if h.scalar(`SELECT COUNT(*) FROM billing_catalog_versions WHERE state='published'`) == 0 {
		h.exec(`INSERT INTO billing_catalog_versions(state,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at) VALUES('published','[]','test','test','test',?,?,?)`, inside.UnixMilli(), inside.UnixMilli(), inside.UnixMilli())
	}
	bank := func(id, environment, status string) {
		t.Helper()
		h.exec(`INSERT INTO billing_orders(id,user_id,idempotency_key,catalog_version,product_code,snapshot_json,amount_minor,currency,provider,environment,bank_order,status,checkout_token_hash,request_json,created_at,updated_at)
			SELECT ?,?,?,id,'pack30','{"code":"pack30","kind":"pack","units":30,"price_minor":13000}',13000,'KZT','bcc',?,?,?,'private-bank-hash','{"private":"private-bank-request"}',?,? FROM billing_catalog_versions WHERE state='published'`,
			id, second.userID, id, environment, "private-bank-order-"+id, status, inside.UnixMilli(), now.UnixMilli())
	}
	bank("bank-paid", "production", "succeeded")
	bank("bank-test", "sandbox", "succeeded")
	bank("bank-refunded", "production", "refunded")
	h.exec(`INSERT INTO payments(id,user_id,plan_id,provider,amount,currency,status,created_at,updated_at) SELECT 'breakdown-demo',?,id,'demo',399000,'KZT','succeeded',?,? FROM plans WHERE code='free'`, first.userID, inside.UnixMilli(), now.UnixMilli())

	dashboard := h.adminCall(admin, http.MethodGet, "/dashboard?range=30d", nil)
	products := purchaseBreakdownByCode(t, dashboard)
	if dashboard.num("stats", "purchases_range") != 3 || dashboard.num("stats", "buyers_range") != 2 || len(products) != 2 {
		t.Fatalf("summary totals must use the same completed production set: %s", dashboard.raw)
	}
	pack := products["pack30"]
	if pack["purchases"] != float64(2) || pack["buyers"] != float64(2) || pack["unknown_price_purchases"] != float64(1) {
		t.Fatalf("Google price cannot be invented from today's catalogue: %v", pack)
	}
	if amounts := purchaseAmountsByCurrency(t, pack); len(amounts) != 1 || amounts["KZT"] != 130000 {
		t.Fatalf("bank paid amount must use its immutable order, not catalog 777KZT: %v", pack)
	}
	if missing := products["retired_package"]; missing == nil || missing["purchases"] != float64(1) || missing["buyers"] != float64(0) || missing["name"] == "" {
		t.Fatalf("missing catalogue/deleted account must not erase a sale: %v", missing)
	}
	history := h.adminCall(admin, http.MethodGet, "/dashboard/purchases?range=30d&limit=100", nil)
	rows := reportingRows(t, history, "purchases")
	if history.num("total") != 3 || len(rows) != 3 {
		t.Fatalf("history and summary must agree: %s", history.raw)
	}
	for _, row := range rows {
		if row["product_code"] == "retired_package" && row["user_id"] != "" {
			t.Errorf("deleted account cannot have a profile link: %v", row)
		}
		if row["provider"] == "google_play" && (row["price_known"] != false || row["amount_milliunits"] != float64(0)) {
			t.Errorf("unknown Google amount must stay unknown: %v", row)
		}
		if row["provider"] == "bcc" && (row["price_known"] != true || row["amount_milliunits"] != float64(130000) || row["units"] != float64(30)) {
			t.Errorf("bank purchase must show the sold pack and amount: %v", row)
		}
	}
	for _, secret := range []string{"private-tx-", "private-original-id", "private-sealed-token", "private-bank-hash", "private-bank-request", "private-bank-order-"} {
		if strings.Contains(string(history.raw), secret) || strings.Contains(string(dashboard.raw), secret) {
			t.Errorf("report exposed %s", secret)
		}
	}
	// Choosing an empty historical month must refresh both totals and rows.
	empty := h.adminCall(admin, http.MethodGet, "/dashboard/purchases?range=custom&from=2000-01-01&to=2000-01-31", nil)
	if rows := reportingRows(t, empty, "purchases"); empty.num("total") != 0 || len(rows) != 0 {
		t.Fatalf("custom range must scope history: %s", empty.raw)
	}
	for _, query := range []string{"limit=100000", "limit=0", "limit=nope", "page=0", "page=1000001", "page=nope"} {
		bad := h.adminCall(admin, http.MethodGet, "/dashboard/purchases?"+query, nil)
		mustStatus(t, bad, http.StatusBadRequest, "INVALID_REQUEST")
	}
}

func TestAdminPurchaseBreakdownIncludesTheEntireSelectedLocalDay(t *testing.T) {
	h := newHarness(t)
	admin := h.signInAdmin()
	user := h.signIn("breakdown-day-boundary@example.com")
	loc := h.cfg.App.Location()
	local := h.clock.Now().In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -1)
	next := day.AddDate(0, 0, 1)
	for i, purchased := range []time.Time{day, next.Add(-time.Millisecond), next} {
		h.exec(`INSERT INTO iap_purchases(id,user_id,provider,environment,product_code,transaction_id,quantity,units,status,purchased_at,created_at,updated_at,price_milliunits,currency)
			VALUES(?,?,'app_store','Production','pack30',?,1,30,'granted',?,?,?,130000,'KZT')`,
			fmt.Sprintf("boundary-%d", i), user.userID, fmt.Sprintf("boundary-transaction-%d", i), purchased.UnixMilli(), purchased.UnixMilli(), purchased.UnixMilli())
	}
	query := "?range=custom&from=" + day.Format("2006-01-02") + "&to=" + day.Format("2006-01-02")
	dashboard := h.adminCall(admin, http.MethodGet, "/dashboard"+query, nil)
	products := purchaseBreakdownByCode(t, dashboard)
	if dashboard.num("stats", "purchases_range") != 2 || products["pack30"]["purchases"] != float64(2) {
		t.Fatalf("selected day must include 23:59:59.999 in app timezone: %s", dashboard.raw)
	}
	history := h.adminCall(admin, http.MethodGet, "/dashboard/purchases"+query, nil)
	if rows := reportingRows(t, history, "purchases"); history.num("total") != 2 || len(rows) != 2 {
		t.Fatalf("history and summary date boundaries must agree: %s", history.raw)
	}
}

func TestAdminPurchaseHistoryRequiresAdminSession(t *testing.T) {
	h := newHarness(t)
	user := h.signIn("breakdown-non-admin@example.com")
	for _, headers := range []map[string]string{nil, h.auth(user.access)} {
		res := h.do(http.MethodGet, "/api/v1/admin/dashboard/purchases", nil, headers)
		mustStatus(t, res, http.StatusUnauthorized, "UNAUTHORIZED")
	}
}
