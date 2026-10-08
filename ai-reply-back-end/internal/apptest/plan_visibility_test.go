package apptest

import (
	"context"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/payments"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/migrations"
)

// openStore — сатып алуды ашады: тарифтер көрінеді, әкімші ауыстырғышы қосулы.
func (h *harness) openStore(codes ...string) {
	h.t.Helper()
	for _, code := range codes {
		if _, err := h.db.Writer().Exec(`UPDATE plans SET is_visible = 1 WHERE code = ?`, code); err != nil {
			h.t.Fatal(err)
		}
	}
	if err := h.store.SetSetting(context.Background(), payments.SettingPurchasesEnabled, "true"); err != nil {
		h.t.Fatal(err)
	}
}

// adminPlan — әкімші тізіміндегі тариф жолы (панель оны дәл осылай қайта жібереді).
func (h *harness) adminPlan(admin adminSession, code string) map[string]any {
	h.t.Helper()
	res := h.do(http.MethodGet, "/api/v1/admin/plans", nil, admin.headers(h.cfg.Admin.CookieName))
	if res.status != http.StatusOK {
		h.t.Fatalf("admin plans: %d %s", res.status, res.raw)
	}
	list, _ := res.body["plans"].([]any)
	for _, item := range list {
		if plan, _ := item.(map[string]any); plan["code"] == code {
			return plan
		}
	}
	h.t.Fatalf("plan %q missing", code)
	return nil
}

// savePlan — Plans бетінің «Сақтау» батырмасы: өзгертілген жолды PATCH етеді.
func (h *harness) savePlan(admin adminSession, row map[string]any, changes map[string]any) response {
	h.t.Helper()
	for key, value := range changes {
		row[key] = value
	}
	return h.do(http.MethodPatch, "/api/v1/admin/plans/"+row["id"].(string), row, admin.headers(h.cfg.Admin.CookieName))
}

// listedCodes — қолданбаға көрінетін тарифтер (GET /api/v1/plans).
func (h *harness) listedCodes() (codes []string, purchasable map[string]bool, enabled bool) {
	h.t.Helper()
	res := h.do(http.MethodGet, "/api/v1/plans", nil, nil)
	if res.status != http.StatusOK {
		h.t.Fatalf("plans: %d %s", res.status, res.raw)
	}
	purchasable = map[string]bool{}
	list, _ := res.body["plans"].([]any)
	for _, item := range list {
		plan, _ := item.(map[string]any)
		code, _ := plan["code"].(string)
		codes = append(codes, code)
		purchasable[code], _ = plan["purchasable"].(bool)
	}
	enabled, _ = res.body["purchases_enabled"].(bool)
	return codes, purchasable, enabled
}

func (h *harness) checkout(s session, code string) response {
	return h.do(http.MethodPost, "/api/v1/payments/checkout", map[string]any{"plan_id": h.planID(code)}, h.auth(s.access))
}

// Жаңа қолданушы тіркелген бойда тегін тарифте: ештеңе таңдамайды, ештеңе төлемейді.
func TestNewUserStartsOnTheFreePlan(t *testing.T) {
	h := newHarness(t)
	mustStatus(t, h.requestEmailCode("first@example.com"), http.StatusOK, "")
	verify := h.verifyEmailCode("first@example.com", "1111")
	if verify.status != http.StatusOK {
		t.Fatalf("verify: %d %s", verify.status, verify.raw)
	}
	access := verify.str("access_token")

	sub := h.do(http.MethodGet, "/api/v1/me/subscription", nil, h.auth(access))
	if sub.status != http.StatusOK || sub.str("plan", "code") != "free" || sub.str("status") != "active" ||
		sub.str("source") != "system" {
		t.Fatalf("subscription = %d %s", sub.status, sub.raw)
	}
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(access))
	if usage.num("daily_limit") != 7 || usage.num("remaining_today") != 7 {
		t.Fatalf("usage = %s", usage.raw)
	}
	if res := h.generate(access, "Сәлем, бағасы қанша?"); res.status != http.StatusOK {
		t.Fatalf("first reply: %d %s", res.status, res.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM payments`); n != 0 {
		t.Fatalf("payments = %d, the free plan needs none", n)
	}
}

// Бірінші релиз: тек тегін тариф көрінеді, ақылы тарифтер мен сатып алу жабық.
func TestFirstReleaseShowsOnlyTheFreePlan(t *testing.T) {
	h := newHarness(t)

	codes, purchasable, enabled := h.listedCodes()
	if strings.Join(codes, ",") != "free" || purchasable["free"] || enabled {
		t.Fatalf("plans = %v purchasable = %v enabled = %v", codes, purchasable, enabled)
	}
	config := h.do(http.MethodGet, "/api/v1/config", nil, nil)
	if features, _ := config.body["features"].(map[string]any); features["purchases"] != false {
		t.Fatalf("features.purchases = %v", features["purchases"])
	}

	// The landing page lists the same plans as the apps: no paid prices.
	for _, locale := range []string{"en", "ru", "kk"} {
		page := h.do(http.MethodGet, "/?lang="+locale, nil, nil)
		body := string(page.raw)
		for _, price := range []string{traits.FormatMoney(199000, "KZT"), traits.FormatMoney(349000, "KZT")} {
			if strings.Contains(body, price) {
				t.Fatalf("landing %s shows %q", locale, price)
			}
		}
	}

	// Existing subscribers are counted, nothing was deleted.
	if n := h.scalar(`SELECT COUNT(*) FROM plans`); n != 3 {
		t.Fatalf("plans in the database = %d", n)
	}
}

// Әкімші тегін лимитті өзгертеді — қолданушы оны бірден алады, сервер сол шекті ұстайды.
func TestAdminChangesTheFreeLimit(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("free-limit@example.com")
	admin := h.signInAdmin()

	if res := h.savePlan(admin, h.adminPlan(admin, "free"), map[string]any{"daily_message_limit": 10}); res.status != http.StatusOK {
		t.Fatalf("save free plan: %d %s", res.status, res.raw)
	}
	if limit := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access)).num("daily_limit"); limit != 10 {
		t.Fatalf("daily limit = %v, want 10", limit)
	}
	for i := 0; i < 10; i++ {
		if res := h.generate(s.access, "Сәлем, бағасы қанша?"); res.status != http.StatusOK {
			t.Fatalf("reply %d: %d %s", i+1, res.status, res.raw)
		}
	}
	mustStatus(t, h.generate(s.access, "Сәлем, бағасы қанша?"), http.StatusTooManyRequests, "DAILY_LIMIT_REACHED")

	// Lowering it below what was already used leaves nothing for today.
	if res := h.savePlan(admin, h.adminPlan(admin, "free"), map[string]any{"daily_message_limit": 7}); res.status != http.StatusOK {
		t.Fatalf("save free plan: %d %s", res.status, res.raw)
	}
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	if usage.num("daily_limit") != 7 || usage.num("remaining_today") != 0 {
		t.Fatalf("usage = %s", usage.raw)
	}
	if row := h.adminPlan(admin, "free"); row["is_visible"] != true || row["is_active"] != true || row["is_default"] != true {
		t.Fatalf("free plan row = %v", row)
	}
}

// Жасырын, өшірулі, мұрағаттағы не тегін тарифті API арқылы да сатып алуға болмайды.
func TestHiddenPlansCannotBeBought(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("buyer@example.com")
	admin := h.signInAdmin()
	// Buying is open, but the paid plans are still hidden.
	if err := h.store.SetSetting(context.Background(), payments.SettingPurchasesEnabled, "true"); err != nil {
		t.Fatal(err)
	}

	mustStatus(t, h.checkout(s, "standard"), http.StatusConflict, "PLAN_UNAVAILABLE")
	mustStatus(t, h.checkout(s, "pro"), http.StatusConflict, "PLAN_UNAVAILABLE")
	mustStatus(t, h.checkout(s, "free"), http.StatusConflict, "PLAN_UNAVAILABLE")
	mustStatus(t, h.do(http.MethodPost, "/api/v1/payments/checkout", map[string]any{"plan_id": "no-such-plan"},
		h.auth(s.access)), http.StatusConflict, "PLAN_UNAVAILABLE")

	// Visible but disabled is not for sale either.
	if res := h.savePlan(admin, h.adminPlan(admin, "standard"), map[string]any{"is_visible": true, "is_active": false}); res.status != http.StatusOK {
		t.Fatalf("save: %d %s", res.status, res.raw)
	}
	mustStatus(t, h.checkout(s, "standard"), http.StatusConflict, "PLAN_UNAVAILABLE")
	// Archived neither.
	if res := h.savePlan(admin, h.adminPlan(admin, "pro"), map[string]any{"is_visible": true}); res.status != http.StatusOK {
		t.Fatalf("save: %d %s", res.status, res.raw)
	}
	if res := h.do(http.MethodPost, "/api/v1/admin/plans/"+h.planID("pro")+"/archive", map[string]any{},
		admin.headers(h.cfg.Admin.CookieName)); res.status != http.StatusOK {
		t.Fatalf("archive: %d %s", res.status, res.raw)
	}
	mustStatus(t, h.checkout(s, "pro"), http.StatusConflict, "PLAN_UNAVAILABLE")

	if n := h.scalar(`SELECT COUNT(*) FROM payments`); n != 0 {
		t.Fatalf("payments = %d, refused checkouts must not leave records", n)
	}
	if code := h.do(http.MethodGet, "/api/v1/me/subscription", nil, h.auth(s.access)).str("plan", "code"); code != "free" {
		t.Fatalf("plan = %q", code)
	}
}

// Тарифті көрсету сатып алуды қоспайды: оны әкімшінің бөлек ауыстырғышы ашады.
func TestShowingAPlanDoesNotStartSelling(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("visible@example.com")
	admin := h.signInAdmin()

	if res := h.savePlan(admin, h.adminPlan(admin, "standard"), map[string]any{"is_visible": true}); res.status != http.StatusOK {
		t.Fatalf("show standard: %d %s", res.status, res.raw)
	}
	codes, purchasable, enabled := h.listedCodes()
	if strings.Join(codes, ",") != "free,standard" || purchasable["standard"] || enabled {
		t.Fatalf("plans = %v purchasable = %v enabled = %v", codes, purchasable, enabled)
	}
	mustStatus(t, h.checkout(s, "standard"), http.StatusForbidden, "PURCHASES_DISABLED")

	open := h.do(http.MethodPost, "/api/v1/admin/settings/purchases", map[string]any{"enabled": true},
		admin.headers(h.cfg.Admin.CookieName))
	if open.status != http.StatusOK || open.body["enabled"] != true || open.body["checkout_available"] != true {
		t.Fatalf("open purchases: %d %s", open.status, open.raw)
	}
	if _, purchasable, enabled = h.listedCodes(); !purchasable["standard"] || purchasable["free"] || !enabled {
		t.Fatalf("purchasable = %v enabled = %v", purchasable, enabled)
	}

	// A checkout started while open cannot be finished once buying is closed.
	started := h.checkout(s, "standard")
	if started.status != http.StatusOK {
		t.Fatalf("checkout: %d %s", started.status, started.raw)
	}
	if res := h.do(http.MethodPost, "/api/v1/admin/settings/purchases", map[string]any{"enabled": false},
		admin.headers(h.cfg.Admin.CookieName)); res.status != http.StatusOK {
		t.Fatalf("close purchases: %d %s", res.status, res.raw)
	}
	mustStatus(t, h.do(http.MethodPost, "/api/v1/payments/"+started.str("payment_id")+"/confirm", map[string]any{},
		h.auth(s.access)), http.StatusForbidden, "PURCHASES_DISABLED")
	if code := h.do(http.MethodGet, "/api/v1/me/subscription", nil, h.auth(s.access)).str("plan", "code"); code != "free" {
		t.Fatalf("plan = %q", code)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action = 'settings.purchases.update'`); n != 2 {
		t.Fatalf("audit entries = %d", n)
	}
}

// Төлем интеграциясы жоқ серверде ауыстырғыш қосылмайды (өндірістегі бірінші релиз осындай).
func TestPurchasesNeedABillingIntegration(t *testing.T) {
	h := newHarness(t, withEnv("PAYMENT_DEMO_CHECKOUT", "false"))
	s := h.signIn("no-billing@example.com")
	admin := h.signInAdmin()

	settings := h.do(http.MethodGet, "/api/v1/admin/settings", nil, admin.headers(h.cfg.Admin.CookieName))
	purchases, _ := settings.body["purchases"].(map[string]any)
	if purchases["checkout_available"] != false || purchases["enabled"] != false || purchases["live"] != false {
		t.Fatalf("purchases = %v", purchases)
	}
	res := h.do(http.MethodPost, "/api/v1/admin/settings/purchases", map[string]any{"enabled": true},
		admin.headers(h.cfg.Admin.CookieName))
	mustStatus(t, res, http.StatusConflict, "PURCHASES_DISABLED")

	// Even a switch flipped straight in the database sells nothing.
	h.openStore("standard", "pro")
	if _, purchasable, enabled := h.listedCodes(); purchasable["standard"] || purchasable["pro"] || enabled {
		t.Fatalf("purchasable = %v enabled = %v", purchasable, enabled)
	}
	mustStatus(t, h.checkout(s, "pro"), http.StatusForbidden, "PURCHASES_DISABLED")
	if n := h.scalar(`SELECT COUNT(*) FROM payments`); n != 0 {
		t.Fatalf("payments = %d", n)
	}
}

// Әкімші тарифті қайта көрсетеді — қолданба оны келесі сұраныста көреді, қайта орнатусыз.
func TestPlanVisibilityReachesTheAppsWithoutARelease(t *testing.T) {
	h := newHarness(t)
	admin := h.signInAdmin()

	if codes, _, _ := h.listedCodes(); strings.Join(codes, ",") != "free" {
		t.Fatalf("before = %v", codes)
	}
	if res := h.savePlan(admin, h.adminPlan(admin, "pro"), map[string]any{"is_visible": true}); res.status != http.StatusOK {
		t.Fatalf("show pro: %d %s", res.status, res.raw)
	}
	if codes, _, _ := h.listedCodes(); strings.Join(codes, ",") != "free,pro" {
		t.Fatalf("after show = %v", codes)
	}

	// An admin tab opened before is_visible existed sends no such field: the
	// stored value stays as it is.
	legacy := h.adminPlan(admin, "pro")
	delete(legacy, "is_visible")
	if res := h.savePlan(admin, legacy, map[string]any{"daily_message_limit": 60}); res.status != http.StatusOK {
		t.Fatalf("legacy save: %d %s", res.status, res.raw)
	}
	if codes, _, _ := h.listedCodes(); strings.Join(codes, ",") != "free,pro" {
		t.Fatalf("after legacy save = %v", codes)
	}

	if res := h.savePlan(admin, h.adminPlan(admin, "pro"), map[string]any{"is_visible": false}); res.status != http.StatusOK {
		t.Fatalf("hide pro: %d %s", res.status, res.raw)
	}
	if codes, _, _ := h.listedCodes(); strings.Join(codes, ",") != "free" {
		t.Fatalf("after hide = %v", codes)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action = 'plan.update' AND entity_id = ?`, h.planID("pro")); n != 3 {
		t.Fatalf("plan audit entries = %d", n)
	}
}

// Тарифті жасыру жазылушыларды, олардың лимитін және төлем тарихын өзгертпейді.
func TestHidingAPlanKeepsSubscribersAndPayments(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("subscriber@example.com")
	admin := h.signInAdmin()
	paymentID := h.buy(s, "pro", 1)

	if res := h.savePlan(admin, h.adminPlan(admin, "pro"), map[string]any{"is_visible": false}); res.status != http.StatusOK {
		t.Fatalf("hide pro: %d %s", res.status, res.raw)
	}
	if res := h.do(http.MethodPost, "/api/v1/admin/settings/purchases", map[string]any{"enabled": false},
		admin.headers(h.cfg.Admin.CookieName)); res.status != http.StatusOK {
		t.Fatalf("close purchases: %d %s", res.status, res.raw)
	}

	sub := h.do(http.MethodGet, "/api/v1/me/subscription", nil, h.auth(s.access))
	if sub.str("plan", "code") != "pro" || sub.str("status") != "active" || sub.str("expires_at") == "" {
		t.Fatalf("subscription = %s", sub.raw)
	}
	if limit := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access)).num("daily_limit"); limit != 50 {
		t.Fatalf("daily limit = %v, want 50", limit)
	}
	if res := h.generate(s.access, "Сәлем, бағасы қанша?"); res.status != http.StatusOK {
		t.Fatalf("reply on a hidden plan: %d %s", res.status, res.raw)
	}
	if status := h.text(`SELECT status FROM payments WHERE id = ?`, paymentID); status != "succeeded" {
		t.Fatalf("payment status = %q", status)
	}
	if row := h.adminPlan(admin, "pro"); row["subscribers"] != float64(1) || row["listed"] != false {
		t.Fatalf("pro row = %v", row)
	}
	// Repeating the confirm does not restart the period.
	expires := sub.str("expires_at")
	h.clock.Advance(24 * time.Hour)
	s = h.refresh(s)
	if res := h.do(http.MethodPost, "/api/v1/payments/"+paymentID+"/confirm", map[string]any{}, h.auth(s.access)); res.status != http.StatusOK {
		t.Fatalf("repeat confirm: %d %s", res.status, res.raw)
	}
	if again := h.do(http.MethodGet, "/api/v1/me/subscription", nil, h.auth(s.access)).str("expires_at"); again != expires {
		t.Fatalf("expires_at moved from %s to %s", expires, again)
	}
}

// Әдепкі тегін тарифті өшіруге не мұрағаттауға болмайды: тіркелу соған сүйенеді.
func TestTheDefaultPlanStaysEnabled(t *testing.T) {
	h := newHarness(t)
	admin := h.signInAdmin()

	res := h.savePlan(admin, h.adminPlan(admin, "free"), map[string]any{"is_active": false})
	mustStatus(t, res, http.StatusBadRequest, "INVALID_REQUEST")
	errBody, _ := res.body["error"].(map[string]any)
	if details, _ := errBody["details"].(map[string]any); details["reason"] != "default_plan" {
		t.Fatalf("details = %s", res.raw)
	}
	mustStatus(t, h.savePlan(admin, h.adminPlan(admin, "free"), map[string]any{"is_free": false, "price": 100}),
		http.StatusBadRequest, "INVALID_REQUEST")
	mustStatus(t, h.do(http.MethodPost, "/api/v1/admin/plans/"+h.planID("free")+"/archive", map[string]any{},
		admin.headers(h.cfg.Admin.CookieName)), http.StatusBadRequest, "INVALID_REQUEST")

	// Hiding it is allowed: the apps simply list no plans, new accounts still get it.
	if res := h.savePlan(admin, h.adminPlan(admin, "free"), map[string]any{"is_visible": false}); res.status != http.StatusOK {
		t.Fatalf("hide free: %d %s", res.status, res.raw)
	}
	if codes, _, _ := h.listedCodes(); len(codes) != 0 {
		t.Fatalf("plans = %v", codes)
	}
	s := h.signIn("after-hide@example.com")
	if code := h.do(http.MethodGet, "/api/v1/me/subscription", nil, h.auth(s.access)).str("plan", "code"); code != "free" {
		t.Fatalf("plan = %q", code)
	}
}

// 0012 бар дерекқорға қолданылады: жазылымдар мен төлемдер өзгеріссіз, ақылы тарифтер жасырын.
func TestPlanVisibilityMigrationKeepsSubscriptions(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "old.db"), MaxReadConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	before := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") && e.Name() < "0012" {
			body, _ := fs.ReadFile(migrations.FS, e.Name())
			before[e.Name()] = &fstest.MapFile{Data: body}
		}
	}
	if _, err := database.Migrate(ctx, db, before); err != nil {
		t.Fatalf("old migrations: %v", err)
	}
	for _, stmt := range []string{
		`INSERT INTO users (id, email, status, created_at, updated_at) VALUES ('u-pro', 'pro@example.com', 'active', 1000, 1000)`,
		`INSERT INTO subscriptions (id, user_id, plan_id, status, source, started_at, expires_at, created_at, updated_at)
		 SELECT 's-pro', 'u-pro', id, 'active', 'payment', 1000, 9999999999999, 1000, 1000 FROM plans WHERE code = 'pro'`,
		`INSERT INTO payments (id, user_id, plan_id, provider, provider_ref, amount, currency, status, created_at, updated_at)
		 SELECT 'p-pro', 'u-pro', id, 'demo', 'demo-1', 349000, 'KZT', 'succeeded', 1000, 1000 FROM plans WHERE code = 'pro'`,
	} {
		if _, err := db.Writer().ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	applied, err := database.Migrate(ctx, db, migrations.FS)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if len(applied) == 0 || applied[0] != "0012_plan_visibility.sql" {
		t.Fatalf("applied = %v", applied)
	}

	query := func(q string) string {
		var v string
		if err := db.Reader().QueryRowContext(ctx, q).Scan(&v); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return v
	}
	if got := query(`SELECT group_concat(code || '=' || is_visible, ',') FROM (SELECT code, is_visible FROM plans ORDER BY sort_order)`); got != "free=1,standard=0,pro=0" {
		t.Fatalf("visibility = %s", got)
	}
	if got := query(`SELECT status || ':' || source FROM subscriptions WHERE id = 's-pro'`); got != "active:payment" {
		t.Fatalf("subscription = %s", got)
	}
	if got := query(`SELECT status || ':' || amount FROM payments WHERE id = 'p-pro'`); got != "succeeded:349000" {
		t.Fatalf("payment = %s", got)
	}
	if got := query(`SELECT value FROM system_settings WHERE key = 'purchases_enabled'`); got != "false" {
		t.Fatalf("purchases_enabled = %s", got)
	}
}
