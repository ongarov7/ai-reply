package apptest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// giveCredits — әкімші панелінен қолмен берілген кредит.
func (h *harness) giveCredits(admin adminSession, userID string, units int, reason string) response {
	return h.do(http.MethodPost, "/api/v1/admin/users/"+userID+"/credits",
		map[string]any{"units": units, "reason": reason}, admin.headers(h.cfg.Admin.CookieName))
}

// auditMeta — әкімші журналындағы әрекеттің соңғы жазбасының метадерегі (жоқ болса nil).
func (h *harness) auditMeta(action string) map[string]any {
	h.t.Helper()
	entries, _, err := h.admin.AuditLog(context.Background(), traits.NewPage(100, 0))
	if err != nil {
		h.t.Fatalf("audit: %v", err)
	}
	for _, e := range entries {
		if e.Action == action {
			return e.Metadata
		}
	}
	return nil
}

// Әкімші бонусты өзгертсе — тек кейінгі тіркелгілерге; өзгеріс аудитте.
func TestWelcomeBonusSettingAppliesToLaterAccountsOnly(t *testing.T) {
	h := newHarness(t, withWelcomeBonus(50))
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	early := h.signIn("welcome-early@example.com")
	cutoff := func() int {
		return h.scalar(`SELECT CAST(value AS INTEGER) FROM system_settings WHERE key = 'welcome_bonus_since'`)
	}
	released := cutoff()

	settings := h.do(http.MethodGet, "/api/v1/admin/settings", nil, headers)
	if settings.num("welcome_bonus", "units") != 50 || settings.num("welcome_bonus", "default") != 20 ||
		settings.num("welcome_bonus", "range", "max") != 1000 || settings.str("welcome_bonus", "since") == "" {
		t.Fatalf("settings = %s", settings.raw)
	}
	noCSRF := h.do(http.MethodPost, "/api/v1/admin/settings/welcome-bonus", map[string]any{"units": 20},
		map[string]string{"Cookie": h.cfg.Admin.CookieName + "=" + admin.cookie})
	mustStatus(t, noCSRF, http.StatusForbidden, "CSRF_MISMATCH")
	for _, units := range []int{-1, 1001} {
		bad := h.do(http.MethodPost, "/api/v1/admin/settings/welcome-bonus", map[string]any{"units": units}, headers)
		mustStatus(t, bad, http.StatusBadRequest, "INVALID_REQUEST")
		if bad.str("error", "details", "field") != "units" {
			t.Fatalf("units %d: %s", units, bad.raw)
		}
	}
	saved := h.do(http.MethodPost, "/api/v1/admin/settings/welcome-bonus", map[string]any{"units": 20}, headers)
	if saved.status != http.StatusOK || saved.num("units") != 20 {
		t.Fatalf("save: %d %s", saved.status, saved.raw)
	}
	if meta := h.auditMeta("settings.welcome_bonus.update"); meta == nil || meta["before"] != float64(50) || meta["after"] != float64(20) ||
		meta["since_before"] == nil || meta["since_after"] != meta["since_before"] {
		t.Fatalf("audit = %v", meta)
	}

	later := h.signIn("welcome-later@example.com")
	if h.welcomeLeft(later.userID) != 20 || h.welcomeLeft(early.userID) != 50 {
		t.Fatalf("welcome: later %d, early %d", h.welcomeLeft(later.userID), h.welcomeLeft(early.userID))
	}
	mustStatus(t, h.do(http.MethodPost, "/api/v1/admin/settings/welcome-bonus", map[string]any{"units": 0}, headers),
		http.StatusOK, "")
	none := h.signIn("welcome-none@example.com")
	if h.welcomeLeft(none.userID) != -1 || h.welcomeLeft(early.userID) != 50 {
		t.Fatal("switching the bonus off touched what was given or still gave one")
	}
	if cutoff() != released {
		t.Fatal("changing or switching off the bonus moved the cutoff")
	}

	// Switched on again: the cutoff moves to that moment, so an account that
	// signed up while it was off never gets it, not even through /me.
	time.Sleep(2 * time.Millisecond) // created_at and the cutoff are milliseconds
	on := h.do(http.MethodPost, "/api/v1/admin/settings/welcome-bonus", map[string]any{"units": 30}, headers)
	if on.status != http.StatusOK || on.num("units") != 30 {
		t.Fatalf("switch on: %d %s", on.status, on.raw)
	}
	moved := cutoff()
	if created := h.scalar(`SELECT created_at FROM users WHERE id = ?`, none.userID); moved <= released || created >= moved {
		t.Fatalf("cutoff %d (was %d), account created %d", moved, released, created)
	}
	if meta := h.auditMeta("settings.welcome_bonus.update"); meta == nil || meta["before"] != float64(0) || meta["after"] != float64(30) ||
		meta["since_after"] != traits.FromMillis(int64(moved)).Format(time.RFC3339Nano) || meta["since_before"] == meta["since_after"] {
		t.Fatalf("switch-on audit = %v", meta)
	}
	me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(none.access))
	if usage, _ := me.body["usage"].(map[string]any); me.status != http.StatusOK || usage["welcome"] != nil {
		t.Fatalf("/me of an account from the off period: %s", me.raw)
	}
	h.refresh(none)
	if h.welcomeLeft(none.userID) != -1 {
		t.Fatal("switching the bonus back on gave it to an account from the off period")
	}
	if again := h.signIn("welcome-again@example.com"); h.welcomeLeft(again.userID) != 30 || h.welcomeLeft(early.userID) != 50 {
		t.Fatalf("after switching on: new %d, early %d", h.welcomeLeft(again.userID), h.welcomeLeft(early.userID))
	}
	mustStatus(t, h.do(http.MethodPost, "/api/v1/admin/settings/welcome-bonus", map[string]any{"units": 40}, headers),
		http.StatusOK, "")
	if cutoff() != moved {
		t.Fatal("a change between two positive values moved the cutoff")
	}
}

// Әкімші қолмен береді және тоқтатады: тексеру, аудит, баланс, журнал.
func TestAdminGrantsAndRevokesCreditsWithAudit(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 705 222 33 01")
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)

	for name, c := range map[string]struct {
		units          int
		reason, field  string
		status         int
		code, wantWhat string
	}{
		"zero":         {0, "Өтемақы", "units", http.StatusBadRequest, "INVALID_REQUEST", "range"},
		"too many":     {10001, "Өтемақы", "units", http.StatusBadRequest, "INVALID_REQUEST", "range"},
		"no reason":    {5, "   ", "reason", http.StatusBadRequest, "INVALID_REQUEST", "required"},
		"long reason":  {5, strings.Repeat("ә", 201), "reason", http.StatusBadRequest, "INVALID_REQUEST", "too_long"},
		"unknown user": {5, "Өтемақы", "", http.StatusNotFound, "NOT_FOUND", ""},
	} {
		userID := s.userID
		if name == "unknown user" {
			userID = "00000000-0000-4000-8000-000000000000"
		}
		res := h.giveCredits(admin, userID, c.units, c.reason)
		mustStatus(t, res, c.status, c.code)
		if res.str("error", "details", "field") != c.field || res.str("error", "details", "reason") != c.wantWhat {
			t.Fatalf("%s: %s", name, res.raw)
		}
	}
	mustStatus(t, h.do(http.MethodPost, "/api/v1/admin/users/"+s.userID+"/credits",
		map[string]any{"units": 5, "reason": "x", "kind": "pack"}, headers), http.StatusBadRequest, "INVALID_REQUEST")

	granted := h.giveCredits(admin, s.userID, 15, "Компенсация за сбой 12.10")
	if granted.status != http.StatusOK || granted.num("grant", "units_total") != 15 || granted.str("grant", "kind") != "admin" {
		t.Fatalf("grant: %d %s", granted.status, granted.raw)
	}
	grantID := granted.str("grant", "id")
	if meta := h.auditMeta("credits.grant"); meta == nil || meta["units"] != float64(15) ||
		meta["reason"] != "Компенсация за сбой 12.10" || meta["grant_id"] != grantID {
		t.Fatalf("grant audit = %v", meta)
	}
	if me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access)); me.num("usage", "credits") != 15 ||
		me.num("usage", "remaining_total") != 22 {
		t.Fatalf("/me after the grant: %s", me.raw)
	}

	detail := h.do(http.MethodGet, "/api/v1/admin/users/"+s.userID, nil, headers)
	grants, _ := detail.body["grants"].([]any)
	ledger, _ := detail.body["ledger"].([]any)
	if detail.num("balances", "credits") != 15 || detail.num("balances", "admin") != 15 ||
		detail.num("balances", "remaining_total") != 22 || len(grants) != 1 || len(ledger) != 1 {
		t.Fatalf("user detail: %s", detail.raw)
	}
	if g := grants[0].(map[string]any); g["status"] != "active" || g["by"] != "admin" || g["units_remaining"] != float64(15) {
		t.Fatalf("grant row = %v", g)
	}
	for _, forbidden := range []string{"reply", "message", "prompt", "conversation", "source_text", "reservation"} {
		if strings.Contains(string(detail.raw), forbidden) {
			t.Fatalf("admin payload exposes %q", forbidden)
		}
	}

	// One generation from the grant, then the rest is revoked.
	h.useQuota(s.userID, 7)
	if res := h.generate(s.access, "Сәлем"); res.str("usage", "bucket") != "admin" || res.num("usage", "credits") != 14 {
		t.Fatalf("generation from the grant: %s", res.raw)
	}
	revoke := h.do(http.MethodPost, "/api/v1/admin/users/"+s.userID+"/credits/"+grantID+"/revoke",
		map[string]any{"reason": "Ошибочное начисление"}, headers)
	if revoke.status != http.StatusOK || revoke.num("units_revoked") != 14 || revoke.str("grant", "status") != "revoked" {
		t.Fatalf("revoke: %d %s", revoke.status, revoke.raw)
	}
	if meta := h.auditMeta("credits.revoke"); meta == nil || meta["units_revoked"] != float64(14) || meta["grant_id"] != grantID {
		t.Fatalf("revoke audit = %v", meta)
	}
	again := h.do(http.MethodPost, "/api/v1/admin/users/"+s.userID+"/credits/"+grantID+"/revoke", map[string]any{}, headers)
	mustStatus(t, again, http.StatusConflict, "CONFLICT")
	if again.str("error", "details", "field") != "grant_id" {
		t.Fatalf("second revoke: %s", again.raw)
	}
	other := h.signIn("+7 705 222 33 02")
	mustStatus(t, h.do(http.MethodPost, "/api/v1/admin/users/"+other.userID+"/credits/"+grantID+"/revoke",
		map[string]any{}, headers), http.StatusNotFound, "NOT_FOUND")
	mustStatus(t, h.generate(s.access, "Тағы"), http.StatusTooManyRequests, "DAILY_LIMIT_REACHED")

	credits := h.do(http.MethodGet, "/api/v1/me/credits", nil, h.auth(s.access))
	entries, _ := credits.body["entries"].([]any)
	if credits.status != http.StatusOK || credits.num("balance", "credits") != 0 || len(entries) != 3 {
		t.Fatalf("/me/credits: %s", credits.raw)
	}
	for i, want := range []struct {
		entry string
		delta float64
	}{{"revoke", -14}, {"consume", -1}, {"grant", 15}} {
		e := entries[i].(map[string]any)
		if e["entry"] != want.entry || e["delta"] != want.delta || e["kind"] != "admin" || e["created_at"] == "" {
			t.Fatalf("entry %d = %v, want %v", i, e, want)
		}
	}
	if strings.Contains(string(credits.raw), "reservation") || strings.Contains(string(credits.raw), grantID) {
		t.Fatalf("/me/credits exposes internal ids: %s", credits.raw)
	}
}

// Әкімші тізімі әр қолданушының барлық қалдығын көрсетеді: тариф + бонус + кредиттер.
func TestAdminUserListShowsTheTotalRemaining(t *testing.T) {
	h := newHarness(t, withWelcomeBonus(10))
	s := h.signIn("list-total@example.com")
	now := h.clock.Now()
	h.addGrant(s.userID, domain.GrantAdmin, 5, now, nil, now)
	h.useQuota(s.userID, 3)
	res := h.do(http.MethodGet, "/api/v1/admin/users?q=list-total", nil, h.signInAdmin().headers(h.cfg.Admin.CookieName))
	rows, _ := res.body["users"].([]any)
	if res.status != http.StatusOK || len(rows) != 1 || rows[0].(map[string]any)["remaining_total"] != float64(19) {
		t.Fatalf("list = %s", res.raw)
	}
}

// Тізім мен карта бір санайды: мерзімі өткен тариф — тегін тариф, айлық шек күнді қысқартады.
func TestAdminUserListCountsWhatTheUserGetsNow(t *testing.T) {
	h := newHarness(t)
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	_, month := h.todayKeys()

	lapsed := h.signIn("list-lapsed@example.com")
	h.assignPlan(admin, lapsed.userID, "standard", "")
	h.exec(`UPDATE subscriptions SET expires_at = ? WHERE user_id = ? AND status = 'active'`,
		traits.Millis(h.clock.Now().Add(-time.Hour)), lapsed.userID)
	h.useQuota(lapsed.userID, 3)

	capped := h.signIn("list-capped@example.com")
	h.assignPlan(admin, capped.userID, "standard", "")
	h.exec(`UPDATE plans SET monthly_message_limit = 40 WHERE code = 'standard'`)
	h.useQuota(capped.userID, 2)
	h.exec(`INSERT INTO usage_monthly (user_id, usage_month, used, updated_at) VALUES (?,?,38,0)`, capped.userID, month)

	list := h.do(http.MethodGet, "/api/v1/admin/users?q=list-", nil, headers)
	rows, _ := list.body["users"].([]any)
	if list.status != http.StatusOK || len(rows) != 2 {
		t.Fatalf("list = %s", list.raw)
	}
	want := map[string][2]float64{lapsed.userID: {7, 4}, capped.userID: {30, 2}}
	for _, raw := range rows {
		row := raw.(map[string]any)
		id, _ := row["id"].(string)
		detail := h.do(http.MethodGet, "/api/v1/admin/users/"+id, nil, headers)
		if row["daily_limit"] != want[id][0] || row["remaining_total"] != want[id][1] ||
			detail.num("entitlement", "daily_limit") != want[id][0] || detail.num("balances", "remaining_total") != want[id][1] {
			t.Fatalf("row %v, want daily %v and total %v; detail %s", row, want[id][0], want[id][1], detail.raw)
		}
	}

	// Sorting by today's use is accepted (it used to fail on a missing column).
	for dir, first := range map[string]string{"desc": lapsed.userID, "asc": capped.userID} {
		sorted := h.do(http.MethodGet, "/api/v1/admin/users?q=list-&sort=used_today&dir="+dir, nil, headers)
		rows, _ := sorted.body["users"].([]any)
		if sorted.status != http.StatusOK || len(rows) != 2 || rows[0].(map[string]any)["id"] != first {
			t.Fatalf("sort=used_today dir=%s: %d %s", dir, sorted.status, sorted.raw)
		}
	}
}
