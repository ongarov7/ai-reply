package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

// Explicit fixture: never read .env or inherit any service credentials.
func billingPrimaryFixture(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		App:                   config.App{Env: "test", Port: 8084, Host: "127.0.0.1", PublicBaseURL: "https://billing.example.invalid", Timezone: "Asia/Almaty", SimulatorEnabled: true},
		Database:              config.Database{Path: filepath.Join(t.TempDir(), "primary.sqlite"), MaxReadConns: 2, BusyTimeout: time.Second, MigrateOnStart: true},
		Auth:                  config.Auth{AccessSecret: strings.Repeat("a", 40), RefreshSecret: strings.Repeat("b", 40), Issuer: "isolation-test", AccessTTL: 15 * time.Minute, RefreshTTL: 24 * time.Hour, DemoMode: true, DemoOTP: "1111", LegacyEnabled: true, LegacySecret: strings.Repeat("c", 40), OTPTTL: 5 * time.Minute, OTPMaxAttempts: 5, OTPRequestsPerHour: 10, OTPResendCooldown: time.Second, OTPRequestsPerDay: 10, OTPMaxFailedPerDay: 10, OTPChannel: "stub"},
		Admin:                 config.Admin{BootstrapEmail: "operator@example.invalid", BootstrapPassword: "local-isolation-fixture-password", CookieName: "aireply_admin", SessionTTL: time.Hour, SecureCookies: true},
		OpenAI:                config.OpenAI{APIKey: "sk-local-fixture", Model: "local-fixture", BaseURL: "http://127.0.0.1:1", Timeout: time.Second, MaxOutputTokens: 180},
		Payments:              config.Payments{Mode: "demo", DemoCheckout: true},
		IAP:                   config.IAP{AppleBundleID: "kz.example.reply", AppleEnabled: false, AppleAcceptSandbox: false},
		Push:                  config.Push{MaxAttempts: 3, QuotaLowPercent: 80, BatchSize: 10, Concurrency: 1},
		Limits:                config.Limits{SourceTextChars: 400, InstructionChars: 400, RequestBodyBytes: 8192, OTPRequestPerHour: 100, OTPVerifyPerHour: 100, AdminLoginPerHour: 100, GenericPerMinute: 100, AIPerMinute: 100, IAPPerMinute: 100, EventsPerMinute: 100, ReportsPerHour: 100, SupportPerMinute: 100},
		BillingSandboxEnabled: true,
	}
}

func TestBillingSandboxDerivesIsolatedValidatedConfiguration(t *testing.T) {
	primary := billingPrimaryFixture(t)
	if problems := primary.Validate(); len(problems) > 0 {
		t.Fatalf("invalid fixture: %v", problems)
	}
	sandbox, err := billingSandboxConfig(primary)
	if err != nil {
		t.Fatal(err)
	}
	if sandbox.Database.Path != filepath.Join(filepath.Dir(primary.Database.Path), "primary-sandbox.sqlite") || sandbox.Database.Path == primary.Database.Path {
		t.Fatal("database path not isolated")
	}
	if sandbox.Auth.AccessSecret == primary.Auth.AccessSecret || sandbox.Auth.RefreshSecret == primary.Auth.RefreshSecret || sandbox.Auth.AccessSecret == sandbox.Auth.RefreshSecret {
		t.Fatal("authentication keys reused")
	}
	if sandbox.Auth.Issuer == primary.Auth.Issuer || sandbox.Admin.CookieName == primary.Admin.CookieName {
		t.Fatal("authentication namespace reused")
	}
	if sandbox.App.PublicBaseURL != "https://billing.example.invalid/sandbox" || sandbox.App.Env != "staging" {
		t.Fatal("sandbox URL/environment mismatch")
	}
	if sandbox.Auth.DemoMode || sandbox.Auth.LegacyEnabled || sandbox.Auth.LegacySecret != "" || sandbox.App.SimulatorEnabled || sandbox.Payments.DemoCheckout || sandbox.Payments.Mode != "off" {
		t.Fatal("sandbox exposed a demo or legacy payment/auth path")
	}
	if !sandbox.IAP.AppleEnabled || !sandbox.IAP.AppleAcceptSandbox || sandbox.IAP.Google() || sandbox.IAP.GoogleVoidedPoll != 0 {
		t.Fatal("sandbox store verification not isolated")
	}
	if sandbox.Push.Enabled || sandbox.Push.WorkerEnabled || sandbox.Push.EmailsEnabled || sandbox.Push.FCM.Configured() || sandbox.BillingSandboxEnabled || sandbox.BCCSandboxNotify.Enabled || sandbox.BCCCheckout.Enabled {
		t.Fatal("sandbox inherited side-effect services")
	}
	if problems := sandbox.Validate(); len(problems) > 0 {
		t.Fatalf("derived configuration invalid: %v", problems)
	}
	// Derivation must not mutate the original configuration or rotate its keys.
	if primary.Auth.AccessSecret != strings.Repeat("a", 40) || !primary.Auth.DemoMode || !primary.Auth.LegacyEnabled || primary.Database.Path == sandbox.Database.Path {
		t.Fatal("primary configuration mutated")
	}
	again, err := billingSandboxConfig(primary)
	if err != nil || again.Auth.AccessSecret != sandbox.Auth.AccessSecret || again.Auth.RefreshSecret != sandbox.Auth.RefreshSecret {
		t.Fatal("derived signing keys changed across restarts")
	}
	for _, secret := range []struct{ primary, sandbox string }{{primary.Auth.AccessSecret, sandbox.Auth.AccessSecret}, {primary.Auth.RefreshSecret, sandbox.Auth.RefreshSecret}} {
		token, err := auth.SignJWT(secret.primary, auth.Claims{Subject: "same-user", Issuer: primary.Auth.Issuer, Type: "access", ExpiresAt: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = auth.ParseJWT(secret.primary, token, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err = auth.ParseJWT(secret.sandbox, token, time.Now()); err == nil {
			t.Fatal("cross-environment signature accepted")
		}
	}
}

func TestBillingSandboxRejectsMemoryAndDatabaseAliases(t *testing.T) {
	for _, path := range []string{"", ":memory:"} {
		cfg := billingPrimaryFixture(t)
		cfg.Database.Path = path
		if _, err := billingSandboxConfig(cfg); err == nil {
			t.Fatalf("accepted database %q", path)
		}
	}
	for _, alias := range []string{"symlink", "hardlink"} {
		t.Run(alias, func(t *testing.T) {
			cfg := billingPrimaryFixture(t)
			sandboxPath := strings.TrimSuffix(cfg.Database.Path, ".sqlite") + "-sandbox.sqlite"
			sentinel := []byte("primary-database-sentinel")
			if err := os.WriteFile(cfg.Database.Path, sentinel, 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			if alias == "symlink" {
				err = os.Symlink(cfg.Database.Path, sandboxPath)
			} else {
				err = os.Link(cfg.Database.Path, sandboxPath)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := billingSandboxConfig(cfg); err == nil {
				t.Fatal("same-inode database accepted")
			}
			got, err := os.ReadFile(cfg.Database.Path)
			if err != nil || string(got) != string(sentinel) {
				t.Fatal("configuration check modified primary data")
			}
		})
	}
	for _, base := range []string{"https://billing.example.invalid/other", "https://user:password@billing.example.invalid", "https://billing.example.invalid?key=value", "ftp://billing.example.invalid"} {
		cfg := billingPrimaryFixture(t)
		cfg.App.PublicBaseURL = base
		if _, err := billingSandboxConfig(cfg); err == nil {
			t.Fatalf("accepted unsafe base URL %q", base)
		}
	}
}

func billingRequest(handler http.Handler, method, path, body, contentType string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func billingAdminLogin(t *testing.T, handler http.Handler, cfg config.Config, prefix string) *http.Cookie {
	t.Helper()
	form := billingRequest(handler, http.MethodGet, prefix+"/admin/login", "", "")
	if form.Code != http.StatusOK || !strings.Contains(form.Body.String(), `action="`+prefix+`/admin/login"`) || !strings.Contains(form.Body.String(), prefix+"/static/admin.css") {
		t.Fatalf("login page prefix: %d %s", form.Code, form.Body.String())
	}
	var csrf *http.Cookie
	for _, cookie := range form.Result().Cookies() {
		if cookie.Name == "aireply_csrf" {
			csrf = cookie
		}
	}
	if csrf == nil || csrf.Path != prefix+"/admin" || !csrf.Secure || !csrf.HttpOnly {
		t.Fatal("login CSRF cookie not scoped securely")
	}
	invalid := billingRequest(handler, http.MethodPost, prefix+"/admin/login", "email=operator%40example.invalid", "application/x-www-form-urlencoded")
	if invalid.Code != http.StatusForbidden {
		t.Fatal("login accepted missing CSRF")
	}
	values := url.Values{"email": {cfg.Admin.BootstrapEmail}, "password": {cfg.Admin.BootstrapPassword}, "csrf": {csrf.Value}}
	login := billingRequest(handler, http.MethodPost, prefix+"/admin/login", values.Encode(), "application/x-www-form-urlencoded", csrf)
	if login.Code != http.StatusSeeOther || login.Header().Get("Location") != prefix+"/admin" {
		t.Fatalf("login redirect: %d %s", login.Code, login.Header().Get("Location"))
	}
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == cfg.Admin.CookieName {
			expectedPath := "/"
			if prefix != "" {
				expectedPath = prefix + "/"
			}
			if cookie.Path != expectedPath || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatal("admin session cookie not scoped securely")
			}
			return cookie
		}
	}
	t.Fatal("missing admin session cookie")
	return nil
}

func billingOpenFixtureDB(t *testing.T, path string) *database.DB {
	t.Helper()
	db, err := database.Open(database.Options{Path: path, MaxReadConns: 2, BusyTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestBillingSandboxBootSeparatesDataSessionsAndPrefixedAdmin(t *testing.T) {
	cfg := billingPrimaryFixture(t)
	sandboxCfg, err := billingSandboxConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	var apps []*application
	t.Cleanup(func() {
		cancel()
		done := make(chan struct{})
		go func() { workers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("application workers did not stop after cancellation")
		}
		for i := len(apps) - 1; i >= 0; i-- {
			apps[i].close()
		}
	})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	primary, err := buildApplication(ctx, cfg, log, &workers)
	if err != nil {
		t.Fatal(err)
	}
	apps = append(apps, primary)
	primaryDB := billingOpenFixtureDB(t, cfg.Database.Path)
	primaryRepo := repository.New(primaryDB)
	user, err := primaryRepo.CreateUser(ctx, domain.User{ID: "shared-test-user", Email: "primary@example.invalid", Status: domain.UserActive})
	if err != nil {
		t.Fatal(err)
	}
	_, err = primaryRepo.InsertCreditGrant(ctx, domain.CreditGrant{UserID: user.ID, Kind: domain.GrantPack, UnitsTotal: 60, UnitsRemaining: 60}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sandbox, err := buildApplication(ctx, sandboxCfg, log, &workers)
	if err != nil {
		t.Fatal(err)
	}
	apps = append(apps, sandbox)
	primary.mux.Handle("/sandbox/", http.StripPrefix("/sandbox", sandbox.handler))
	sandboxDB := billingOpenFixtureDB(t, sandboxCfg.Database.Path)
	sandboxRepo := repository.New(sandboxDB)
	if _, err := sandboxRepo.UserByID(ctx, user.ID); err == nil {
		t.Fatal("primary user appeared in sandbox")
	}
	_, err = sandboxRepo.CreateUser(ctx, domain.User{ID: user.ID, Email: "sandbox@example.invalid", Status: domain.UserActive})
	if err != nil {
		t.Fatal(err)
	}
	balance, err := primaryRepo.CreditBalance(ctx, user.ID, time.Now())
	if err != nil || balance.Packs != 60 {
		t.Fatalf("primary purchased credits changed: %+v %v", balance, err)
	}
	sandboxBalance, err := sandboxRepo.CreditBalance(ctx, user.ID, time.Now())
	if err != nil || sandboxBalance.Packs != 0 {
		t.Fatal("primary purchased credits leaked into sandbox")
	}
	for _, prefix := range []string{"", "/sandbox"} {
		response := billingRequest(primary.handler, http.MethodGet, prefix+"/api/v1/config", "", "")
		if response.Code != http.StatusOK {
			t.Fatalf("config %q: %d %s", prefix, response.Code, response.Body.String())
		}
		for key, want := range map[string]string{"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY"} {
			if response.Header().Get(key) != want {
				t.Fatalf("%s missing through mount", key)
			}
		}
		if response.Header().Get("Strict-Transport-Security") == "" || response.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("security policy missing through mount")
		}
	}
	for _, path := range []string{"/sandbox/simulator", "/sandbox/simulator/login"} {
		if response := billingRequest(primary.handler, http.MethodGet, path, "", ""); response.Code != http.StatusNotFound {
			t.Fatalf("public simulator route %s exposed: %d", path, response.Code)
		}
	}
	if response := billingRequest(primary.handler, http.MethodPost, "/sandbox/v1/auth/register", `{"install_id":"sandbox-public-demo-disabled"}`, "application/json"); response.Code != http.StatusNotFound {
		t.Fatal("public legacy registration exposed in sandbox")
	}
	if response := billingRequest(primary.handler, http.MethodGet, "/sandbox/api/v1/admin/billing/catalog", "", ""); response.Code != http.StatusUnauthorized {
		t.Fatal("anonymous catalog administration exposed")
	}
	primaryCookie := billingAdminLogin(t, primary.handler, cfg, "")
	sandboxCookie := billingAdminLogin(t, primary.handler, sandboxCfg, "/sandbox")
	if primaryCookie.Value == sandboxCookie.Value {
		t.Fatal("session token reused")
	}
	for _, check := range []struct {
		path   string
		cookie *http.Cookie
	}{{"/api/v1/admin/billing/catalog", primaryCookie}, {"/sandbox/api/v1/admin/billing/catalog", sandboxCookie}} {
		if response := billingRequest(primary.handler, http.MethodGet, check.path, "", "", check.cookie); response.Code != http.StatusOK {
			t.Fatalf("own admin session rejected: %d %s", response.Code, response.Body.String())
		}
	}
	for _, check := range []struct{ path, name, value string }{{"/sandbox/api/v1/admin/billing/catalog", sandboxCfg.Admin.CookieName, primaryCookie.Value}, {"/api/v1/admin/billing/catalog", cfg.Admin.CookieName, sandboxCookie.Value}} {
		if response := billingRequest(primary.handler, http.MethodGet, check.path, "", "", &http.Cookie{Name: check.name, Value: check.value}); response.Code != http.StatusUnauthorized {
			t.Fatal("cross-environment admin session accepted")
		}
	}
	page := billingRequest(primary.handler, http.MethodGet, "/sandbox/admin/purchases", "", "", sandboxCookie)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "/sandbox/static/admin-app.js") || !strings.Contains(page.Body.String(), `"base_path":"/sandbox"`) {
		t.Fatalf("sandbox admin app prefix missing (status %d)", page.Code)
	}
	if static := billingRequest(primary.handler, http.MethodGet, "/sandbox/static/admin-app.js", "", ""); static.Code != http.StatusOK {
		t.Fatal("sandbox assets not served under prefix")
	}
	for _, check := range []struct {
		cfg           config.Config
		prefix, email string
	}{{cfg, "", "primary@example.invalid"}, {sandboxCfg, "/sandbox", "sandbox@example.invalid"}} {
		token, err := auth.SignJWT(check.cfg.Auth.AccessSecret, auth.Claims{Subject: user.ID, Issuer: check.cfg.Auth.Issuer, Type: "access", ExpiresAt: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodGet, check.prefix+"/api/v1/me", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		primary.handler.ServeHTTP(response, request)
		var data struct {
			User struct {
				Email string `json:"email"`
			} `json:"user"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &data) != nil || data.User.Email != check.email {
			t.Fatalf("own user session: %d %s", response.Code, response.Body.String())
		}
		otherPrefix := "/sandbox"
		if check.prefix != "" {
			otherPrefix = ""
		}
		request = httptest.NewRequest(http.MethodGet, otherPrefix+"/api/v1/me", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response = httptest.NewRecorder()
		primary.handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatal("cross-environment access token accepted for same user ID")
		}
	}
}

func TestBuildApplicationFailureCanBeRetriedAndWorkersStop(t *testing.T) {
	cfg := billingPrimaryFixture(t)
	cfg.Database.MigrateOnStart = false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var workers sync.WaitGroup
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Missing schema fails before any background worker starts; the owned DB must close.
	if app, err := buildApplication(ctx, cfg, log, &workers); err == nil || app != nil {
		t.Fatal("startup accepted missing schema")
	}
	cfg.Database.MigrateOnStart = true
	app, err := buildApplication(ctx, cfg, log, &workers)
	if err != nil {
		t.Fatalf("startup retry: %v", err)
	}
	cancel()
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("workers survived application cancellation")
	}
	app.close()
}
