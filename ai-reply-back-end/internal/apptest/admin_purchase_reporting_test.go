package apptest

import (
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/iap/appstore/appstoretest"
)

func reportingRows(t *testing.T, res response, key string) []map[string]any {
	t.Helper()
	mustStatus(t, res, http.StatusOK, "")
	items, ok := res.body[key].([]any)
	if !ok {
		t.Fatalf("missing %s rows: %s", key, res.raw)
	}
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("invalid %s row: %v", key, item)
		}
		rows = append(rows, row)
	}
	return rows
}

func assertReportingDashboard(t *testing.T, h *harness, admin adminSession, paid, free int, mix map[string]int) {
	t.Helper()
	res := h.adminCall(admin, http.MethodGet, "/dashboard", nil)
	mustStatus(t, res, http.StatusOK, "")
	if res.num("stats", "paid_users") != float64(paid) || res.num("stats", "free_users") != float64(free) ||
		res.num("stats", "total_users") != float64(paid+free) {
		t.Errorf("dashboard must count each current account once: %s", res.raw)
	}
	series, _ := res.body["series"].(map[string]any)
	points, _ := series["plan_mix"].([]any)
	got := make(map[string]int)
	for _, point := range points {
		p := point.(map[string]any)
		got[p["label"].(string)] += int(p["value"].(float64))
	}
	if !maps.Equal(got, mix) {
		t.Errorf("dashboard plan mix = %v, want %v", got, mix)
	}
	for _, row := range reportingRows(t, h.adminCall(admin, http.MethodGet, "/plans", nil), "plans") {
		code := row["code"].(string)
		if row["subscribers"] != float64(mix[code]) {
			t.Errorf("plan %s subscribers = %v, want %d", code, row["subscribers"], mix[code])
		}
	}
}

func assertReportingUser(t *testing.T, h *harness, admin adminSession, s session, code string) {
	t.Helper()
	me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access))
	mustStatus(t, me, http.StatusOK, "")
	detail := h.adminCall(admin, http.MethodGet, "/users/"+s.userID, nil)
	mustStatus(t, detail, http.StatusOK, "")
	if me.str("subscription", "plan", "code") != code || detail.str("entitlement", "plan_code") != code {
		t.Fatalf("authoritative plan should be %s: me=%s detail=%s", code, me.raw, detail.raw)
	}
	unlimited := code == "pro_monthly" || code == "pro_yearly"
	usage := me.body["usage"].(map[string]any)
	if usage["unlimited"] != unlimited {
		t.Fatalf("plan %s unlimited = %v, want %v", code, usage["unlimited"], unlimited)
	}
	for _, row := range reportingRows(t, h.adminCall(admin, http.MethodGet, "/users?limit=100", nil), "users") {
		if row["id"] != s.userID {
			continue
		}
		if row["plan_code"] != code || row["unlimited"] != unlimited || row["daily_limit"] != me.num("usage", "daily_limit") ||
			row["remaining_total"] != me.num("usage", "remaining_total") || row["used_today"] != me.num("usage", "used_today") {
			t.Errorf("list must match effective plan %s and quota: row=%v usage=%v", code, row, usage)
		}
		if detail.num("entitlement", "daily_limit") != me.num("usage", "daily_limit") ||
			detail.num("balances", "remaining_total") != me.num("usage", "remaining_total") {
			t.Errorf("admin detail must match mobile quota: detail=%s me=%s", detail.raw, me.raw)
		}
		return
	}
	t.Errorf("account %s missing from admin list", s.userID)
}

// A paid store order lives outside the legacy subscriptions table. Every admin
// view must nevertheless show the same effective tariff and quota as the app.
func TestAdminPurchaseReportingMatchesVerifiedMobileAndBankEntitlements(t *testing.T) {
	h := googleHarness(t)
	h.exec(`UPDATE iap_products SET apple_product_id='test.'||code,is_active=1,off_ios=0 WHERE code IN ('pack30','pro_monthly','pro_yearly')`)
	admin := h.signInAdmin()
	now, end := h.clock.Now(), h.clock.Now().Add(30*24*time.Hour)
	apple := h.signIn("reporting-apple@example.com")
	google := h.signIn("reporting-google@example.com")
	bank := h.signIn("reporting-bank@example.com")
	overlap := h.signIn("reporting-overlap@example.com")
	pack := h.signIn("reporting-pack@example.com")
	fallback := h.signIn("reporting-fallback@example.com")
	mustStatus(t, h.postApple(apple.access, h.subscriptionTx(apple.userID, "report-apple", "report-apple-root", "pro_monthly", now, end, nil)), http.StatusOK, "")
	h.playSubscription(google.userID, googleBase, playToken(1100), end, nil)
	mustStatus(t, h.postGoogle(google.access, googleBase, playToken(1100)), http.StatusOK, "")
	h.bankPro("report-bank", bank.userID, now, end)
	// A previously sold bank daily contract retains its terms after the current
	// catalogue changes Base to permanent monthly bundles.
	h.exec(`UPDATE bcc_subscriptions SET plan_id=?,snapshot_json='{"daily_limit":17,"monthly_limit":0,"unlimited":false,"price_minor":79000}' WHERE id='report-bank'`, h.planID("base_monthly"))
	h.useQuota(bank.userID, 3)
	h.subscribe(overlap.userID, "standard", end)
	mustStatus(t, h.postApple(overlap.access, h.subscriptionTx(overlap.userID, "report-overlap", "report-overlap-root", "pro_yearly", now, end, nil)), http.StatusOK, "")
	h.playSubscription(overlap.userID, googlePro, playToken(1101), end, nil)
	mustStatus(t, h.postGoogle(overlap.access, googlePro, playToken(1101)), http.StatusOK, "")
	h.bankPro("report-overlap-bank", overlap.userID, now, end)
	mustStatus(t, h.postApple(pack.access, h.appleTx(pack.userID, "report-pack", "test.pack30", nil)), http.StatusOK, "")
	h.exec(`DELETE FROM subscriptions WHERE user_id=?`, fallback.userID)

	assertReportingDashboard(t, h, admin, 4, 2, map[string]int{"base_monthly": 2, "pro_monthly": 2, "free": 2})
	for _, tc := range []struct {
		s    session
		code string
	}{{apple, "pro_monthly"}, {google, "base_monthly"}, {bank, "base_monthly"}, {overlap, "pro_monthly"}, {pack, "free"}, {fallback, "free"}} {
		t.Run(tc.code+"/"+tc.s.userID, func(t *testing.T) { assertReportingUser(t, h, admin, tc.s, tc.code) })
	}
	if got := h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE user_id=? AND status='granted'`, overlap.userID); got != 2 {
		t.Fatalf("fixture must retain both independently paid store transactions, got %d", got)
	}

	// Filtering and pagination operate on the selected tariff, before LIMIT.
	for i, s := range []session{apple, google, bank, overlap, pack, fallback} {
		h.exec(`UPDATE users SET created_at=? WHERE id=?`, now.Add(time.Duration(i)*time.Millisecond).UnixMilli(), s.userID)
	}
	for _, tc := range []struct {
		code string
		want []session
	}{{"base_monthly", []session{google, bank}}, {"pro_monthly", []session{apple, overlap}}, {"free", []session{pack, fallback}}} {
		t.Run("filter/"+tc.code, func(t *testing.T) {
			for i, want := range tc.want {
				path := fmt.Sprintf("/users?plan=%s&limit=1&page=%d&dir=asc", url.QueryEscape(h.planID(tc.code)), i+1)
				res := h.adminCall(admin, http.MethodGet, path, nil)
				rows := reportingRows(t, res, "users")
				if res.num("total") != float64(len(tc.want)) || len(rows) != 1 || rows[0]["id"] != want.userID {
					t.Errorf("effective plan filter page %d: %s", i+1, res.raw)
				}
			}
		})
	}
}

func TestAdminPurchaseReportingExcludesLapsedRevokedAndDeletedAccess(t *testing.T) {
	h := googleHarness(t)
	h.exec(`UPDATE iap_products SET apple_product_id='test.'||code,is_active=1,off_ios=0 WHERE code='pro_monthly'`)
	admin := h.signInAdmin()
	now, end := h.clock.Now(), h.clock.Now().Add(30*24*time.Hour)
	active := h.signIn("reporting-active-legacy@example.com")
	legacyExpired := h.signIn("reporting-expired-legacy@example.com")
	appleExpired := h.signIn("reporting-expired-apple@example.com")
	appleRefunded := h.signIn("reporting-refunded-apple@example.com")
	googleExpired := h.signIn("reporting-expired-google@example.com")
	bankRevoked := h.signIn("reporting-revoked-bank@example.com")
	bankFuture := h.signIn("reporting-future-bank@example.com")
	deleted := h.signIn("reporting-deleted@example.com")
	h.subscribe(active.userID, "standard", end)
	h.subscribe(legacyExpired.userID, "pro", now.Add(-time.Minute))
	mustStatus(t, h.postApple(appleExpired.access, h.subscriptionTx(appleExpired.userID, "report-expired", "report-expired-root", "pro_monthly", now.Add(-time.Hour), now, nil)), http.StatusOK, "")
	mustStatus(t, h.postApple(appleRefunded.access, h.subscriptionTx(appleRefunded.userID, "report-refund", "report-refund-root", "pro_monthly", now, end, nil)), http.StatusOK, "")
	refund := h.subscriptionTx(appleRefunded.userID, "report-refund", "report-refund-root", "pro_monthly", now, end, func(tx *appstoretest.Transaction) { tx.RevocationDate = now.UnixMilli() })
	mustStatus(t, h.appleNotification("REFUND", "report-refund-notice", refund), http.StatusOK, "")
	h.playSubscription(googleExpired.userID, googlePro, playToken(1102), end, nil)
	mustStatus(t, h.postGoogle(googleExpired.access, googlePro, playToken(1102)), http.StatusOK, "")
	h.exec(`UPDATE google_subscriptions SET state=?,expires_at=? WHERE user_id=?`, domain.GoogleSubExpired, now.UnixMilli(), googleExpired.userID)
	h.bankPro("report-bank-revoked", bankRevoked.userID, now, end)
	h.exec(`UPDATE bcc_subscription_periods SET revoked_at=? WHERE order_id='report-bank-revoked'`, now.UnixMilli())
	h.bankPro("report-bank-future", bankFuture.userID, now.Add(time.Hour), end)
	h.subscribe(deleted.userID, "pro", end)
	mustStatus(t, h.postApple(deleted.access, h.subscriptionTx(deleted.userID, "report-deleted", "report-deleted-root", "pro_monthly", now, end, nil)), http.StatusOK, "")
	// Keep orphaned billing metadata to ensure reporting explicitly excludes a
	// deleted account instead of depending on account-cleanup side effects.
	h.exec(`UPDATE users SET deleted_at=? WHERE id=?`, now.UnixMilli(), deleted.userID)

	assertReportingDashboard(t, h, admin, 1, 6, map[string]int{"standard": 1, "free": 6})
	assertReportingUser(t, h, admin, active, "standard")
	for _, s := range []session{legacyExpired, appleExpired, appleRefunded, googleExpired, bankRevoked, bankFuture} {
		assertReportingUser(t, h, admin, s, "free")
	}
	res := h.adminCall(admin, http.MethodGet, "/users?plan="+url.QueryEscape(h.planID("pro_monthly")), nil)
	if rows := reportingRows(t, res, "users"); res.num("total") != 0 || len(rows) != 0 {
		t.Errorf("inactive/deleted mobile purchases leaked into paid filter: %s", res.raw)
	}
}

func TestAdminPurchaseReportingPreservesBaseQuotasAndGoogleRenewalAccess(t *testing.T) {
	h := googleHarness(t)
	h.exec(`UPDATE iap_products SET apple_product_id='test.base_monthly',is_active=1,off_ios=0 WHERE code='base_monthly'`)
	admin := h.signInAdmin()
	now, end := h.clock.Now(), h.clock.Now().Add(30*24*time.Hour)
	apple := h.signIn("reporting-apple-base@example.com")
	manual := h.signIn("reporting-manual-base@example.com")
	mustStatus(t, h.postApple(apple.access, h.subscriptionTx(apple.userID, "report-apple-base", "report-apple-base-root", "base_monthly", now, end, nil)), http.StatusOK, "")
	h.subscribe(manual.userID, "base_monthly", end)
	h.useQuota(manual.userID, 7)
	assertReportingUser(t, h, admin, apple, "base_monthly")
	assertReportingUser(t, h, admin, manual, "base_monthly")

	for i, state := range []string{"canceled", "bridged", "on_hold", "revoked", "superseded"} {
		s := h.signIn("reporting-google-" + state + "@example.com")
		token := playToken(1200 + i)
		h.playSubscription(s.userID, googlePro, token, end, nil)
		mustStatus(t, h.postGoogle(s.access, googlePro, token), http.StatusOK, "")
		want := "pro_monthly"
		switch state {
		case "canceled":
			h.exec(`UPDATE google_subscriptions SET state=?,auto_renew=0 WHERE user_id=?`, domain.GoogleSubCanceled, s.userID)
		case "bridged":
			// The paid period just ended while Google's next verification is
			// delayed. Reporting must preserve the same renewal bridge as /me.
			h.exec(`UPDATE google_subscriptions SET expires_at=?,read_at=? WHERE user_id=?`, now.Add(-time.Minute).UnixMilli(), now.Add(-time.Hour).UnixMilli(), s.userID)
		case "on_hold":
			h.exec(`UPDATE google_subscriptions SET state=? WHERE user_id=?`, domain.GoogleSubOnHold, s.userID)
			want = "free"
		case "revoked":
			h.exec(`UPDATE google_subscriptions SET revoked_at=? WHERE user_id=?`, now.UnixMilli(), s.userID)
			want = "free"
		case "superseded":
			h.exec(`UPDATE google_subscriptions SET superseded_by='replacement-token' WHERE user_id=?`, s.userID)
			want = "free"
		}
		assertReportingUser(t, h, admin, s, want)
	}
	assertReportingDashboard(t, h, admin, 4, 3, map[string]int{"base_monthly": 2, "pro_monthly": 2, "free": 3})
}
