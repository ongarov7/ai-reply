package apptest

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/admin"
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/subscriptions"
)

// loginResult — /admin/login не /simulator/login POST нәтижесі.
type loginResult struct {
	status   int
	location string
	session  *http.Cookie
}

// adminLogin — кіру формасын ашып (CSRF cookie), поштамен және құпиясөзбен жібереді.
func (h *harness) adminLogin(area, email, password string) loginResult {
	h.t.Helper()
	return h.adminLoginFrom(area, email, password, "")
}

// adminLoginFrom — сол кіру, X-Forwarded-For арқылы берілген IP-ден (TRUST_PROXY=true болса).
func (h *harness) adminLoginFrom(area, email, password, ip string) loginResult {
	h.t.Helper()
	client := h.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	csrfName := map[string]string{"admin": "aireply_csrf", "simulator": "aireply_sim_csrf"}[area]

	form, err := client.Get(h.server.URL + "/" + area + "/login")
	if err != nil {
		h.t.Fatalf("login form: %v", err)
	}
	form.Body.Close()
	var csrf string
	for _, c := range form.Cookies() {
		if c.Name == csrfName {
			csrf = c.Value
		}
	}
	values := url.Values{"email": {email}, "password": {password}, "csrf": {csrf}}
	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/"+area+"/login", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if ip != "" {
		req.Header.Set("X-Forwarded-For", ip)
	}
	req.AddCookie(&http.Cookie{Name: csrfName, Value: csrf})
	res, err := client.Do(req)
	if err != nil {
		h.t.Fatalf("login: %v", err)
	}
	defer res.Body.Close()
	out := loginResult{status: res.StatusCode, location: res.Header.Get("Location")}
	for _, c := range res.Cookies() {
		if c.Name == h.cfg.Admin.CookieName && c.Value != "" {
			out.session = c
		}
	}
	return out
}

// Бір пошта мен бір IP жұбына 15 минутта 5 әрекет; басқа пошта өз шелегінде.
func TestAdminLoginIsLimitedPerEmail(t *testing.T) {
	h := newHarness(t, withEnv("RATE_ADMIN_LOGIN_PER_HOUR", "100"))
	for i := 0; i < 5; i++ {
		if res := h.adminLogin("admin", adminEmail, "wrong-password"); res.status != http.StatusSeeOther || res.session != nil {
			t.Fatalf("attempt %d: %+v", i+1, res)
		}
	}
	// The sixth attempt from the same source is refused before the password is
	// checked, even the right one, and the simulator's sign-in shares the bucket.
	for _, area := range []string{"admin", "simulator"} {
		if res := h.adminLogin(area, strings.ToUpper(adminEmail), adminPassword); res.status != http.StatusTooManyRequests || res.session != nil {
			t.Fatalf("%s: sixth attempt = %+v", area, res)
		}
	}
	if res := h.adminLogin("admin", "someone-else@aireply.test", "x"); res.status != http.StatusSeeOther {
		t.Fatalf("another address has its own bucket: %+v", res)
	}
}

// Әкімшіні әдейі қателесіп бұғаттау мүмкін емес: пошта шелегі толса да, қателеспеген
// IP-ден дұрыс құпиясөз өтеді; қателескен IP-лер бұғатталады.
func TestAdminLoginCannotBeLockedOutByOthers(t *testing.T) {
	h := newHarness(t, withEnv("RATE_ADMIN_LOGIN_PER_HOUR", "1000"), withEnv("TRUST_PROXY", "true"))
	// 20 failures for the address from four sources fill the account-wide bucket.
	for source := 1; source <= 4; source++ {
		for i := 0; i < 5; i++ {
			ip := "203.0.113." + strconv.Itoa(source)
			if res := h.adminLoginFrom("admin", adminEmail, "wrong-password", ip); res.status != http.StatusSeeOther {
				t.Fatalf("%s attempt %d: %+v", ip, i+1, res)
			}
		}
	}
	// A new source still has its password checked once; after failing it is refused too.
	if res := h.adminLoginFrom("admin", adminEmail, "wrong-again", "198.51.100.7"); res.status != http.StatusSeeOther {
		t.Fatalf("a clean source is checked: %+v", res)
	}
	for _, area := range []string{"admin", "simulator"} {
		if res := h.adminLoginFrom(area, adminEmail, adminPassword, "198.51.100.7"); res.status != http.StatusTooManyRequests || res.session != nil {
			t.Fatalf("%s: a failing source while the address is under attack: %+v", area, res)
		}
	}
	// The administrator, from a source that has not been failing, signs in.
	for _, area := range []string{"admin", "simulator"} {
		res := h.adminLoginFrom(area, adminEmail, adminPassword, "192.0.2.10")
		if res.status != http.StatusSeeOther || res.session == nil || res.location != "/"+area {
			t.Fatalf("%s: the administrator is locked out: %+v", area, res)
		}
	}
}

// Сәтсіз кіру аудитке жазылады (IP-мен, құпиясөзсіз); белгісіз пошта жазылмайды.
func TestAdminLoginFailuresAreAudited(t *testing.T) {
	h := newHarness(t)
	h.adminLogin("admin", adminEmail, "guess-number-one")
	h.adminLogin("admin", "stranger@example.com", "guess-number-two")

	rows, err := h.db.Reader().Query(`SELECT admin_id, admin_email, entity_id, metadata, ip FROM admin_audit_logs
		WHERE action = 'admin.login_failed' ORDER BY created_at, rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var adminID, email, entity, meta, ip string
		if err := rows.Scan(&adminID, &email, &entity, &meta, &ip); err != nil {
			t.Fatal(err)
		}
		if ip != "127.0.0.1" {
			t.Fatalf("ip = %q", ip)
		}
		got = append(got, email+"|"+meta+"|"+map[bool]string{true: "id", false: "no id"}[adminID != "" && adminID == entity])
	}
	want := []string{adminEmail + `|{"reason":"wrong_password"}|id`, `|{"reason":"unknown_email"}|no id`}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("audit rows:\n%s", strings.Join(got, "\n"))
	}
	if h.dbContains("guess-number-one") || h.dbContains("guess-number-two") || h.dbContains("stranger@example.com") {
		t.Fatal("a password or an unknown address reached the database")
	}
}

// Шығу CSRF токенімен ғана: бөгде бет әкімшіні жасырын формамен шығара алмайды.
func TestAdminLogoutRequiresCSRF(t *testing.T) {
	h := newHarness(t)
	for _, area := range []string{"admin", "simulator"} {
		admin := h.signInAdmin()
		post := func(csrf string) int {
			client := h.server.Client()
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/"+area+"/logout",
				strings.NewReader(url.Values{"csrf": {csrf}}.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(&http.Cookie{Name: h.cfg.Admin.CookieName, Value: admin.cookie})
			res, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			return res.StatusCode
		}
		sessionAlive := func() bool {
			return h.do(http.MethodGet, "/api/v1/admin/session", nil,
				map[string]string{"Cookie": h.cfg.Admin.CookieName + "=" + admin.cookie}).status == http.StatusOK
		}
		for _, bad := range []string{"", "forged-token"} {
			if status := post(bad); status != http.StatusForbidden || !sessionAlive() {
				t.Fatalf("%s logout with csrf %q: %d, alive %v", area, bad, status, sessionAlive())
			}
		}
		if status := post(admin.csrf); status != http.StatusSeeOther || sessionAlive() {
			t.Fatalf("%s logout: %d, alive %v", area, status, sessionAlive())
		}
	}
}

// ADMIN_PASSWORD өзгерсе: жаңа хэш, барлық сессия жабылады, аудит жазылады; өзгермесе — ештеңе.
func TestAdminPasswordRotationOnBootstrap(t *testing.T) {
	h := newHarness(t)
	old := h.signInAdmin()
	bootstrap := func(password string) {
		cfg := h.cfg
		cfg.Admin.BootstrapPassword = password
		subs := subscriptions.New(h.store, plans.New(h.store), cfg.App.Location())
		if err := admin.New(h.store, subs, plans.New(h.store), cfg, discardLog()).Bootstrap(context.Background()); err != nil {
			t.Fatalf("bootstrap: %v", err)
		}
	}

	bootstrap(adminPassword) // unchanged: nothing happens
	if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action = 'admin.password_rotated'`); n != 0 {
		t.Fatalf("an unchanged password was rotated: %d", n)
	}

	const rotated = "a-brand-new-admin-passphrase"
	bootstrap(rotated)
	if res := h.do(http.MethodGet, "/api/v1/admin/session", nil,
		map[string]string{"Cookie": h.cfg.Admin.CookieName + "=" + old.cookie}); res.status != http.StatusUnauthorized {
		t.Fatalf("an old session survived the rotation: %d", res.status)
	}
	if res := h.adminLogin("admin", adminEmail, adminPassword); res.session != nil {
		t.Fatal("the old password still signs in")
	}
	if res := h.adminLogin("admin", adminEmail, rotated); res.status != http.StatusSeeOther || res.session == nil {
		t.Fatalf("the new password does not sign in: %+v", res)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action = 'admin.password_rotated'`); n != 1 {
		t.Fatalf("rotation audit rows = %d", n)
	}
	if h.dbContains(rotated) {
		t.Fatal("the new password is stored in clear")
	}
}

// https сайтта әкімші cookie-лері Secure және HSTS жіберіледі; http-де — жоқ.
func TestAdminCookiesAndHSTSFollowHTTPS(t *testing.T) {
	for _, tc := range []struct {
		base   string
		secure bool
	}{{"https://ai-reply.kz", true}, {"http://localhost:8084", false}} {
		h := newHarness(t, withEnv("PUBLIC_BASE_URL", tc.base))
		res := h.adminLogin("admin", adminEmail, adminPassword)
		if res.session == nil || res.session.Secure != tc.secure {
			t.Fatalf("%s: session cookie %+v", tc.base, res.session)
		}
		page := h.do(http.MethodGet, "/", nil, nil)
		if hsts := page.header.Get("Strict-Transport-Security"); (hsts != "") != tc.secure {
			t.Fatalf("%s: HSTS %q", tc.base, hsts)
		}
	}
}

// SIMULATOR_ENABLED=false: симулятордың беті де, API-і де жоқ (404); лендингте сілтеме жоқ.
func TestSimulatorCanBeSwitchedOff(t *testing.T) {
	h := newHarness(t, withEnv("SIMULATOR_ENABLED", "false"))
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	for _, path := range []string{"/simulator", "/simulator/login", "/simulator/app"} {
		if status, _ := fetch(t, h, path); status != http.StatusNotFound {
			t.Errorf("GET %s: %d", path, status)
		}
	}
	if res := h.do(http.MethodPost, "/simulator/login", map[string]any{}, nil); res.status != http.StatusNotFound {
		t.Errorf("POST /simulator/login: %d", res.status)
	}
	for _, path := range []string{"/api/v1/simulator/bootstrap", "/api/v1/simulator/account"} {
		if res := h.do(http.MethodGet, path, nil, headers); res.status != http.StatusNotFound {
			t.Errorf("GET %s: %d", path, res.status)
		}
	}
	if res := h.do(http.MethodPost, "/api/v1/simulator/generate", map[string]any{}, headers); res.status != http.StatusNotFound {
		t.Errorf("POST generate: %d", res.status)
	}

	// The landing never links to it; with the simulator on it is still reachable by URL.
	on := newHarness(t, withEnv("SIMULATOR_ENABLED", "true")) // t.Setenv above outlives the first harness
	for _, srv := range []*harness{h, on} {
		if _, body := fetch(t, srv, "/?lang=en"); strings.Contains(body, `href="/simulator"`) || strings.Contains(body, "Product demo") {
			t.Fatal("the public pages link to the product demo")
		}
	}
	if status, _ := fetch(t, on, "/simulator/login"); status != http.StatusOK {
		t.Fatalf("simulator login with SIMULATOR_ENABLED on: %d", status)
	}
}
