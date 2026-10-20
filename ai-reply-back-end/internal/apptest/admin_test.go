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

// Әкімші API сессиясыз қолжетімсіз.
func TestAdminAPIRequiresSession(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/api/v1/admin/dashboard", "/api/v1/admin/users", "/api/v1/admin/plans"} {
		res := h.do(http.MethodGet, path, nil, nil)
		if res.status != http.StatusUnauthorized {
			t.Fatalf("%s accessible without session: %d", path, res.status)
		}
	}
}

// Қате құпиясөзбен кіру мүмкін емес.
func TestAdminLoginRejectsWrongPassword(t *testing.T) {
	h := newHarness(t)
	client := h.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	form, err := client.Get(h.server.URL + "/admin/login")
	if err != nil {
		t.Fatalf("login form: %v", err)
	}
	defer form.Body.Close()
	var csrf string
	for _, c := range form.Cookies() {
		if c.Name == "aireply_csrf" {
			csrf = c.Value
		}
	}

	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/admin/login",
		strings.NewReader("email="+adminEmail+"&password=wrong-password&csrf="+csrf))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "aireply_csrf", Value: csrf})
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer res.Body.Close()

	for _, c := range res.Cookies() {
		if c.Name == h.cfg.Admin.CookieName && c.Value != "" {
			t.Fatal("session cookie issued for a wrong password")
		}
	}
	if location := res.Header.Get("Location"); !strings.Contains(location, "error") {
		t.Fatalf("expected redirect back to the login form, got %q", location)
	}
}

// CSRF токенсіз күй өзгертуге болмайды.
func TestAdminMutationRequiresCSRF(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 706 111 22 33")
	adminSession := h.signInAdmin()

	res := h.do(http.MethodPost, "/api/v1/admin/users/"+session.userID+"/status",
		map[string]any{"status": "disabled"},
		map[string]string{"Cookie": h.cfg.Admin.CookieName + "=" + adminSession.cookie})
	if res.status != http.StatusForbidden {
		t.Fatalf("mutation without CSRF accepted: %d", res.status)
	}
}

// Тарифтің толық CRUD-ы жұмыс істейді және аудитке жазылады.
func TestPlanCRUDAndAudit(t *testing.T) {
	h := newHarness(t)
	adminSession := h.signInAdmin()
	headers := adminSession.headers(h.cfg.Admin.CookieName)

	created := h.do(http.MethodPost, "/api/v1/admin/plans", map[string]any{
		"code": "team", "name": map[string]string{"en": "Team", "kk": "Команда", "ru": "Команда", "uz": "Jamoa"},
		"description": map[string]string{"en": "For teams"}, "price": 499000, "currency": "KZT",
		"daily_message_limit": 100, "monthly_message_limit": 2000, "period_days": 30,
		"is_free": false, "is_active": true, "sort_order": 40,
	}, headers)
	if created.status != http.StatusCreated {
		t.Fatalf("create plan: %d %s", created.status, created.raw)
	}
	planID := created.str("id")

	duplicate := h.do(http.MethodPost, "/api/v1/admin/plans", map[string]any{
		"code": "team", "name": map[string]string{"en": "Team"}, "daily_message_limit": 10,
	}, headers)
	if duplicate.status != http.StatusConflict {
		t.Fatalf("duplicate code accepted: %d", duplicate.status)
	}

	updated := h.do(http.MethodPatch, "/api/v1/admin/plans/"+planID, map[string]any{
		"code": "team", "name": map[string]string{"en": "Team"}, "description": map[string]string{},
		"price": 599000, "currency": "KZT", "daily_message_limit": 120,
		"monthly_message_limit": 0, "period_days": 30, "is_free": false, "is_active": true, "sort_order": 40,
	}, headers)
	if updated.status != http.StatusOK {
		t.Fatalf("update plan: %d %s", updated.status, updated.raw)
	}

	archived := h.do(http.MethodPost, "/api/v1/admin/plans/"+planID+"/archive", map[string]any{}, headers)
	if archived.status != http.StatusOK {
		t.Fatalf("archive plan: %d %s", archived.status, archived.raw)
	}

	// Мұрағатталған тариф мобильді тізімге кірмейді.
	public := h.do(http.MethodGet, "/api/v1/plans", nil, nil)
	if strings.Contains(string(public.raw), "\"team\"") {
		t.Fatal("archived plan is still offered to clients")
	}

	entries, _, err := h.admin.AuditLog(context.Background(), traits.NewPage(50, 0))
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	actions := map[string]bool{}
	for _, entry := range entries {
		actions[entry.Action] = true
	}
	for _, expected := range []string{"admin.login", "plan.create", "plan.update", "plan.archive"} {
		if !actions[expected] {
			t.Fatalf("audit log missing %q (have %v)", expected, actions)
		}
	}
}

// Дашборд метрикалары нақты деректен жиналады.
func TestDashboardReflectsUsage(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 706 222 33 44")
	for i := 0; i < 3; i++ {
		if res := h.generate(session.access, "Сұраныс"); res.status != http.StatusOK {
			t.Fatalf("generate: %d", res.status)
		}
	}

	adminSession := h.signInAdmin()
	dashboard := h.do(http.MethodGet, "/api/v1/admin/dashboard?range=30d", nil,
		adminSession.headers(h.cfg.Admin.CookieName))
	if dashboard.status != http.StatusOK {
		t.Fatalf("dashboard: %d %s", dashboard.status, dashboard.raw)
	}
	if total := dashboard.num("stats", "total_users"); total < 1 {
		t.Fatalf("total_users = %v", total)
	}
	if requests := dashboard.num("stats", "requests_today"); requests != 3 {
		t.Fatalf("requests_today = %v, want 3", requests)
	}
	if tokens := dashboard.num("stats", "total_tokens"); tokens != 480 {
		t.Fatalf("total_tokens = %v, want 480", tokens)
	}
	if cost := dashboard.num("stats", "cost_usd"); cost <= 0 {
		t.Fatalf("cost_usd = %v, want a positive estimate", cost)
	}
}

// Әкімші квотаны қалпына келтіре алады.
func TestAdminCanResetQuota(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 706 333 44 55")
	for i := 0; i < 7; i++ {
		h.generate(session.access, "Сұраныс")
	}
	if blocked := h.generate(session.access, "Артық"); blocked.errorCode() != "DAILY_LIMIT_REACHED" {
		t.Fatal("limit not reached")
	}

	adminSession := h.signInAdmin()
	reset := h.do(http.MethodPost, "/api/v1/admin/users/"+session.userID+"/reset-quota",
		map[string]any{}, adminSession.headers(h.cfg.Admin.CookieName))
	if reset.status != http.StatusOK {
		t.Fatalf("reset: %d %s", reset.status, reset.raw)
	}
	if res := h.generate(session.access, "Қайта сұраныс"); res.status != http.StatusOK {
		t.Fatalf("after reset: %d %s", res.status, res.raw)
	}
}

// Сессияларды жабу токендерді жарамсыз етеді.
func TestAdminCanRevokeSessions(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 706 444 55 66")
	adminSession := h.signInAdmin()

	revoke := h.do(http.MethodPost, "/api/v1/admin/users/"+session.userID+"/revoke-sessions",
		map[string]any{}, adminSession.headers(h.cfg.Admin.CookieName))
	if revoke.status != http.StatusOK {
		t.Fatalf("revoke: %d %s", revoke.status, revoke.raw)
	}
	after := h.do(http.MethodPost, "/api/v1/auth/refresh",
		map[string]any{"refresh_token": session.refresh}, nil)
	if after.status != http.StatusUnauthorized {
		t.Fatalf("revoked refresh still works: %d", after.status)
	}
}

// Әкімші іздеуі % пен _ таңбаларын әріп ретінде іздейді (ListInstallations сияқты).
func TestAdminUserSearchMatchesWildcardsLiterally(t *testing.T) {
	h := newHarness(t)
	h.signIn("under_score@example.com")
	h.signIn("underxscore@example.com")
	headers := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	for q, want := range map[string]float64{"under_score": 1, "underxscore": 1, "%25": 0, "under%25": 0} {
		res := h.do(http.MethodGet, "/api/v1/admin/users?q="+q, nil, headers)
		if res.status != http.StatusOK || res.num("total") != want {
			t.Errorf("q=%s matched %v users, want %v", q, res.num("total"), want)
		}
	}
}

// Әкімші картасы жазылымның шын күйін көрсетеді: мерзімі өткені «expired», жоғы «none».
func TestAdminUserDetailShowsTheRealSubscriptionState(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("sub-state@example.com")
	headers := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	detail := func() response {
		t.Helper()
		res := h.do(http.MethodGet, "/api/v1/admin/users/"+s.userID, nil, headers)
		mustStatus(t, res, http.StatusOK, "")
		return res
	}
	if res := detail(); res.str("subscription", "status") != "active" || res.body["subscription"].(map[string]any)["usable"] != true {
		t.Fatalf("free plan: %s", res.raw)
	}
	h.exec(`DELETE FROM subscriptions WHERE user_id = ?`, s.userID)
	h.subscribe(s.userID, "pro", h.clock.Now().Add(-time.Hour))
	if res := detail(); res.str("subscription", "status") != "expired" || res.str("entitlement", "plan_code") != "free" ||
		res.body["subscription"].(map[string]any)["usable"] != false || res.str("subscription", "expires_at") == "" {
		t.Fatalf("lapsed plan: %s", res.raw)
	}
	h.exec(`DELETE FROM subscriptions WHERE user_id = ?`, s.userID)
	if res := detail(); res.str("subscription", "status") != "none" {
		t.Fatalf("no subscription: %s", res.raw)
	}
}

// Дашборд: «бүгін» — қолданба белдеуінің күні; лимитке тірелу сәтсіз генерация емес.
func TestDashboardCountsTheAppDayAndKeepsRefusalsApart(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 706 222 33 01")
	for i := 0; i < 6; i++ {
		mustStatus(t, h.generate(s.access, "Сұраныс"), http.StatusOK, "")
	}
	h.provider.err = domain.ErrProviderDown
	mustStatus(t, h.generate(s.access, "Құлады"), http.StatusBadGateway, "AI_PROVIDER_UNAVAILABLE")
	h.provider.err = nil
	mustStatus(t, h.generate(s.access, "Жетінші"), http.StatusOK, "")
	mustStatus(t, h.generate(s.access, "Артық"), http.StatusTooManyRequests, "DAILY_LIMIT_REACHED")
	mustStatus(t, h.generate(s.access, "Тағы артық"), http.StatusTooManyRequests, "DAILY_LIMIT_REACHED")

	headers := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	stats := func() response {
		t.Helper()
		res := h.do(http.MethodGet, "/api/v1/admin/dashboard?range=30d", nil, headers)
		mustStatus(t, res, http.StatusOK, "")
		return res
	}
	before := stats()
	if before.num("stats", "failed") != 1 || before.num("stats", "refused") != 2 || before.num("stats", "succeeded") != 7 ||
		before.num("stats", "requests_today") != 7 {
		t.Fatalf("stats = %s", before.raw)
	}

	// Local midnight in Asia/Almaty is 19:00 UTC of the day before.
	local := h.clock.Now().In(h.cfg.App.Location())
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, h.cfg.App.Location())
	for id, at := range map[string]time.Time{"u-yesterday": midnight.Add(-time.Minute), "u-today": midnight.Add(time.Minute)} {
		h.exec(`INSERT INTO users (id, email, status, created_at, updated_at) VALUES (?, ?, 'active', ?, ?)`,
			id, id+"@example.com", at.UnixMilli(), at.UnixMilli())
	}
	if delta := stats().num("stats", "new_today") - before.num("stats", "new_today"); delta != 1 {
		t.Fatalf("new_today grew by %v, want 1 (only the account after local midnight)", delta)
	}
}
