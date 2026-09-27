// Package apptest — толық стекті ұштан-ұшқа тексеретін тесттер.
package apptest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/admin"
	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/limits"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
	"github.com/aireply/ai-reply-back-end/internal/payments"
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/simulator"
	"github.com/aireply/ai-reply-back-end/internal/subscriptions"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/adminapi"
	"github.com/aireply/ai-reply-back-end/internal/transport/api"
	"github.com/aireply/ai-reply-back-end/internal/transport/simulatorapi"
	"github.com/aireply/ai-reply-back-end/internal/transport/web"
	"github.com/aireply/ai-reply-back-end/internal/users"
	"github.com/aireply/ai-reply-back-end/migrations"
)

// fakeProvider — тестте нақты OpenAI орнына.
type fakeProvider struct {
	reply         string
	err           error
	calls         int
	lastUser      string
	lastDeveloper string
	lastMaxTokens int
}

func (f *fakeProvider) Name() string  { return "fake" }
func (f *fakeProvider) Model() string { return "test-model" }

func (f *fakeProvider) Generate(_ context.Context, prompt ai.Prompt) (ai.Completion, error) {
	f.calls++
	f.lastUser = prompt.User
	f.lastDeveloper = prompt.Developer
	f.lastMaxTokens = prompt.MaxOutputTokens
	if f.err != nil {
		return ai.Completion{}, f.err
	}
	return ai.Completion{
		Text: f.reply, Model: "test-model", InputTokens: 120, OutputTokens: 40, ProviderMS: 12,
	}, nil
}

// harness — жинақталған қолданба.
type harness struct {
	t        *testing.T
	cfg      config.Config
	server   *httptest.Server
	store    *repository.Store
	db       *database.DB
	provider *fakeProvider
	clock    *traits.FixedClock
	logs     *bytes.Buffer
	dbPath   string
	admin    *admin.Service
	limits   *limits.Service
}

const (
	adminEmail    = "admin@aireply.test"
	adminPassword = "super-secret-admin-pass"
)

func newHarness(t *testing.T) *harness {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	env := map[string]string{
		"APP_ENV": "development", "APP_PORT": "0", "PUBLIC_BASE_URL": "http://localhost:8084",
		"SQLITE_PATH": dbPath, "DEFAULT_TIMEZONE": "Asia/Almaty",
		"JWT_ACCESS_SECRET":   "test-access-secret-that-is-long-enough-000",
		"JWT_REFRESH_SECRET":  "test-refresh-secret-that-is-long-enough-0",
		"AUTH_SIGNING_SECRET": "test-legacy-secret-that-is-long-enough-00",
		"ACCESS_TOKEN_TTL":    "15m", "REFRESH_TOKEN_TTL": "720h",
		"OPENAI_API_KEY": "sk-test-key", "OPENAI_MODEL": "test-model",
		"AUTH_DEMO_MODE": "true", "AUTH_DEMO_OTP": "1111",
		"PAYMENT_MODE": "demo", "ADMIN_EMAIL": adminEmail, "ADMIN_PASSWORD": adminPassword,
		"LOG_LEVEL": "info", "LOG_FORMAT": "json", "RATE_AI_PER_MINUTE": "1000",
		"RATE_OTP_REQUEST_PER_HOUR": "100", "RATE_OTP_VERIFY_PER_HOUR": "200",
		"RATE_GENERIC_PER_MINUTE": "1000", "OTP_MAX_ATTEMPTS": "5",
	}
	for key, value := range env {
		t.Setenv(key, value)
	}

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	db, err := database.Open(database.Options{Path: cfg.Database.Path, MaxReadConns: 4})
	if err != nil {
		t.Fatalf("database: %v", err)
	}
	if _, err := database.Migrate(context.Background(), db, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Тест моделінің бағасы (нақты бағалар дерекқорда тұрады, кодта емес).
	if err := repository.New(db).SavePricing(context.Background(), repository.Pricing{
		Model: "test-model", InputPer1M: 0.15, OutputPer1M: 0.60, Currency: "USD",
		EffectiveFrom: time.Unix(0, 0).UTC(),
	}); err != nil {
		t.Fatalf("pricing: %v", err)
	}

	logs := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(io.MultiWriter(logs, io.Discard), &slog.HandlerOptions{Level: slog.LevelDebug}))
	clock := &traits.FixedClock{T: time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)}

	store := repository.New(db)
	planSvc := plans.New(store)
	subSvc := subscriptions.New(store, planSvc, cfg.App.Location()).WithClock(clock)
	userSvc := users.New(store)
	authSvc := auth.New(store, cfg.Auth, auth.StubSender{Log: log}, subSvc, log).WithClock(clock)
	provider := &fakeProvider{reply: "Сәлеметсіз бе! Бағаны нақтылап, бірер минуттан соң жазамын."}
	limitSvc := limits.New(store, limits.Limits{
		SourceChars:      cfg.Limits.SourceTextChars,
		InstructionChars: cfg.Limits.InstructionChars,
		MaxOutputTokens:  cfg.OpenAI.MaxOutputTokens,
	}).WithTTL(0)
	aiSvc := ai.New(store, subSvc, provider, limitSvc, log).WithClock(clock)
	paymentSvc := payments.New(store, subSvc, payments.DemoProvider{}, cfg.Payments.Mode)
	notifySvc := notifications.New(store)
	adminSvc := admin.New(store, subSvc, planSvc, cfg, log)
	simulatorSvc := simulator.New(simulator.Deps{
		Repo: store, Users: userSvc, Subs: subSvc, Plans: planSvc, AI: aiSvc, Limits: limitSvc,
		Config: cfg, Log: log,
	})
	if err := adminSvc.Bootstrap(context.Background()); err != nil {
		t.Fatalf("admin bootstrap: %v", err)
	}

	bundle, err := localization.Load()
	if err != nil {
		t.Fatalf("localization: %v", err)
	}
	limiter := middleware.NewLimiter()

	mux := http.NewServeMux()
	api.New(api.Deps{Config: cfg, Auth: authSvc, Users: userSvc, Plans: planSvc, Subs: subSvc,
		AI: aiSvc, Limits: limitSvc, Payments: paymentSvc, Limiter: limiter, Log: log,
		Ping: func(ctx context.Context) error { return db.Reader().PingContext(ctx) }}).Register(mux)
	adminapi.New(adminapi.Deps{Config: cfg, Admin: adminSvc, Limits: limitSvc, Notifications: notifySvc,
		Log: log}).Register(mux)
	simulatorapi.New(simulatorapi.Deps{Config: cfg, Admin: adminSvc, Simulator: simulatorSvc,
		Limiter: limiter, Log: log}).Register(mux)
	webServer, err := web.New(web.Deps{Config: cfg, Admin: adminSvc, Plans: planSvc,
		Notifications: notifySvc, Bundle: bundle, Limiter: limiter, Log: log})
	if err != nil {
		t.Fatalf("web: %v", err)
	}
	webServer.Register(mux)

	handler := middleware.Chain(mux, middleware.RequestID, middleware.Recover(log), middleware.Logging(log),
		middleware.SecurityHeaders(cfg.App.IsProduction()))
	server := httptest.NewServer(handler)

	h := &harness{t: t, cfg: cfg, server: server, store: store, db: db,
		provider: provider, clock: clock, logs: logs, dbPath: dbPath, admin: adminSvc, limits: limitSvc}
	t.Cleanup(func() {
		server.Close()
		_ = db.Close()
	})
	return h
}

// ---------------------------------------------------------------- helpers

type response struct {
	status int
	body   map[string]any
	raw    []byte
	header http.Header
}

func (r response) errorCode() string {
	if errObj, ok := r.body["error"].(map[string]any); ok {
		if code, ok := errObj["code"].(string); ok {
			return code
		}
	}
	return ""
}

func (r response) str(path ...string) string {
	current := any(r.body)
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[key]
	}
	if s, ok := current.(string); ok {
		return s
	}
	return ""
}

func (r response) num(path ...string) float64 {
	current := any(r.body)
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return 0
		}
		current = m[key]
	}
	if f, ok := current.(float64); ok {
		return f
	}
	return 0
}

func (h *harness) do(method, path string, body any, headers map[string]string) response {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatalf("request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	res, err := h.server.Client().Do(req)
	if err != nil {
		h.t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	parsed := map[string]any{}
	_ = json.Unmarshal(raw, &parsed)
	return response{status: res.StatusCode, body: parsed, raw: raw, header: res.Header}
}

func (h *harness) auth(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

// session — демо OTP арқылы кіріп, токендерді қайтарады.
type session struct {
	access  string
	refresh string
	userID  string
	isNew   bool
}

func (h *harness) signIn(identifier string) session {
	h.t.Helper()
	res := h.do(http.MethodPost, "/api/v1/auth/request-otp",
		map[string]any{"identifier": identifier, "locale": "kk"}, nil)
	if res.status != http.StatusOK {
		h.t.Fatalf("request-otp: status %d body %s", res.status, res.raw)
	}
	verify := h.do(http.MethodPost, "/api/v1/auth/verify-otp", map[string]any{
		"identifier": identifier, "code": "1111",
		"device": map[string]any{"platform": "ios", "app_version": "1.0.0", "locale": "kk"},
	}, nil)
	if verify.status != http.StatusOK {
		h.t.Fatalf("verify-otp: status %d body %s", verify.status, verify.raw)
	}
	isNew, _ := verify.body["is_new_user"].(bool)
	return session{
		access:  verify.str("access_token"),
		refresh: verify.str("refresh_token"),
		userID:  verify.str("user", "id"),
		isNew:   isNew,
	}
}

// refresh — токендерді жаңартады (уақыт озған тесттерде қажет).
func (h *harness) refresh(current session) session {
	h.t.Helper()
	res := h.do(http.MethodPost, "/api/v1/auth/refresh",
		map[string]any{"refresh_token": current.refresh}, nil)
	if res.status != http.StatusOK {
		h.t.Fatalf("refresh: status %d body %s", res.status, res.raw)
	}
	return session{access: res.str("access_token"), refresh: res.str("refresh_token"), userID: current.userID}
}

// generate — AI жауабын сұрайды.
func (h *harness) generate(token, text string) response {
	return h.do(http.MethodPost, "/api/v1/ai/reply", map[string]any{
		"source_text": text, "instruction": "Сыпайы жауап бер", "language": "kk",
		"template_id": "client", "platform": "ios", "app_version": "1.0.0",
	}, h.auth(token))
}

// adminSession — әкімші cookie сессиясы.
type adminSession struct {
	cookie string
	csrf   string
}

func (h *harness) signInAdmin() adminSession {
	h.t.Helper()
	client := h.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	form, err := client.Get(h.server.URL + "/admin/login")
	if err != nil {
		h.t.Fatalf("login form: %v", err)
	}
	defer form.Body.Close()
	var csrfCookie string
	for _, c := range form.Cookies() {
		if c.Name == "aireply_csrf" {
			csrfCookie = c.Value
		}
	}
	if csrfCookie == "" {
		h.t.Fatal("csrf cookie missing on login form")
	}

	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/admin/login",
		bytes.NewReader([]byte("email="+adminEmail+"&password="+adminPassword+"&csrf="+csrfCookie)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "aireply_csrf", Value: csrfCookie})
	res, err := client.Do(req)
	if err != nil {
		h.t.Fatalf("admin login: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		h.t.Fatalf("admin login status %d", res.StatusCode)
	}
	var sessionCookie string
	for _, c := range res.Cookies() {
		if c.Name == h.cfg.Admin.CookieName {
			sessionCookie = c.Value
		}
	}
	if sessionCookie == "" {
		h.t.Fatal("admin session cookie missing")
	}

	session := h.do(http.MethodGet, "/api/v1/admin/session", nil,
		map[string]string{"Cookie": h.cfg.Admin.CookieName + "=" + sessionCookie})
	if session.status != http.StatusOK {
		h.t.Fatalf("admin session: %d", session.status)
	}
	return adminSession{cookie: sessionCookie, csrf: session.str("csrf")}
}

func (a adminSession) headers(cookieName string) map[string]string {
	return map[string]string{
		"Cookie":       cookieName + "=" + a.cookie,
		"X-CSRF-Token": a.csrf,
	}
}

// dbContains — дерекқор файлында мәтіннің бар-жоғын тексереді (құпиялық тесті).
func (h *harness) dbContains(needle string) bool {
	h.t.Helper()
	if err := h.db.Writer().Close(); err == nil {
		_ = err
	}
	raw, err := os.ReadFile(h.dbPath)
	if err != nil {
		h.t.Fatalf("read db: %v", err)
	}
	wal, _ := os.ReadFile(h.dbPath + "-wal")
	return bytes.Contains(raw, []byte(needle)) || bytes.Contains(wal, []byte(needle))
}

func (h *harness) entitlement(userID string) domain.Entitlement {
	h.t.Helper()
	subSvc := subscriptions.New(h.store, plans.New(h.store), h.cfg.App.Location()).WithClock(h.clock)
	ent, err := subSvc.Entitlement(context.Background(), userID)
	if err != nil {
		h.t.Fatalf("entitlement: %v", err)
	}
	return ent
}
