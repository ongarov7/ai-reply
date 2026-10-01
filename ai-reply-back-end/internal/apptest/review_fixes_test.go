package apptest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Анонимді орнату құрылғылар тізімін құлатпайды; идентификатор қысқартылған; тіркелгі сүзгісі.
func TestDeviceListShowsAnonymousInstallations(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("device-list@example.com")
	h.mustRegister(installation(installID(80), "android", fcmToken(80)), s.access)
	h.mustRegister(installation(installID(81), "ios", apnsToken(81)), "")
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)

	all := h.do(http.MethodGet, "/api/v1/admin/notifications/devices", nil, headers)
	if all.status != http.StatusOK || all.num("total") != 2 {
		t.Fatalf("devices: %d %s", all.status, all.raw)
	}
	if strings.Contains(string(all.raw), installID(80)) || strings.Contains(string(all.raw), installID(81)) {
		t.Fatal("full installation ids would let anyone detach the phone")
	}
	for query, want := range map[string]float64{"?auth=anonymous": 1, "?auth=authenticated": 1, "?auth=anonymous&platform=android": 0} {
		if res := h.do(http.MethodGet, "/api/v1/admin/notifications/devices"+query, nil, headers); res.num("total") != want {
			t.Errorf("%s: %s", query, res.raw)
		}
	}
	if res := h.do(http.MethodGet, "/api/v1/admin/notifications/devices?auth=robots", nil, headers); res.status != http.StatusBadRequest {
		t.Fatalf("unknown auth filter: %d", res.status)
	}
}

// Бір Idempotency-Key басқа мазмұнмен қайта келсе — 409, бірінші науқан өзгермейді.
func TestIdempotencyKeyCannotBeReusedForOtherContent(t *testing.T) {
	h := newHarness(t)
	admin := h.signInAdmin()
	headers := withKey(admin.headers(h.cfg.Admin.CookieName), "reused-key-00000001")
	first := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", map[string]any{"title": "Бірінші", "body": "Мәтін"}, headers)
	if first.status != http.StatusCreated || first.str("created_by_email") != adminEmail {
		t.Fatalf("first: %d %s", first.status, first.raw)
	}
	again := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", map[string]any{"title": "Бірінші", "body": "Мәтін"}, headers)
	if again.status != http.StatusOK || again.str("id") != first.str("id") {
		t.Fatalf("a retry returns the first campaign: %d %s", again.status, again.raw)
	}
	other := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", map[string]any{"title": "Басқа", "body": "Мәтін"}, headers)
	if other.status != http.StatusConflict || other.errorCode() != "CONFLICT" || other.str("error", "details", "field") != "idempotency_key" {
		t.Fatalf("different content under the same key: %d %s", other.status, other.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notification_campaigns WHERE idempotency_key = 'reused-key-00000001'`); n != 1 {
		t.Fatalf("campaigns = %d", n)
	}
}

// Refresh токені ескірген болса да, шығу орнатуды тіркелгіден ажыратады.
func TestLogoutWithAnExpiredRefreshTokenStillDetaches(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("stale-logout@example.com")
	h.mustRegister(installation(installID(82), "android", fcmToken(82)), s.access)
	res := h.do(http.MethodPost, "/api/v1/auth/logout", map[string]any{"refresh_token": "no-longer-valid"},
		map[string]string{"X-Installation-ID": installID(82)})
	if res.status != http.StatusOK {
		t.Fatalf("logout: %d %s", res.status, res.raw)
	}
	if got := h.text(`SELECT user_id FROM app_installations WHERE installation_id = ?`, installID(82)); got != "" {
		t.Fatalf("still attached to %q", got)
	}
}

// Шектеу орнату бойынша есептеледі: бір IP-дегі басқа телефон шектелмейді.
func TestInstallationBudgetIsPerInstallation(t *testing.T) {
	h := newHarness(t, withEnv("RATE_GENERIC_PER_MINUTE", "2"))
	register := func(seed int) int {
		return h.do(http.MethodPost, "/api/v1/installations", installation(installID(seed), "android", fcmToken(seed)),
			map[string]string{"X-Installation-ID": installID(seed)}).status
	}
	for i := 0; i < 2; i++ {
		if status := register(83); status != http.StatusOK {
			t.Fatalf("registration %d: %d", i, status)
		}
	}
	if status := register(83); status != http.StatusTooManyRequests {
		t.Fatalf("third registration of the same installation: %d", status)
	}
	if status := register(84); status != http.StatusOK {
		t.Fatalf("another phone behind the same address was throttled: %d", status)
	}
}

// Әкімші панелінің өз қателері api_errors-қа түспейді; аудитте IP қысқартылған.
func TestAdminErrorsStayOutOfAppErrorsAndAuditIPsAreMasked(t *testing.T) {
	h := newHarness(t)
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	if res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", map[string]any{"title": "", "body": ""},
		withKey(headers, "admin-error-0000001")); res.status != http.StatusBadRequest {
		t.Fatalf("invalid campaign: %d", res.status)
	}
	h.telemetry.Flush(context.Background())
	if n := h.scalar(`SELECT COUNT(*) FROM api_errors WHERE route LIKE '/api/v1/admin/%'`); n != 0 {
		t.Fatalf("admin errors recorded as app errors: %d", n)
	}
	h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", map[string]any{"title": "Аудит", "body": "Мәтін"},
		withKey(headers, "admin-audit-0000001"))
	audit := h.do(http.MethodGet, "/api/v1/admin/audit?action=notification.campaign.", nil, headers)
	if strings.Contains(string(audit.raw), "127.0.0.1") || !strings.Contains(string(audit.raw), "127.0.0.x") {
		t.Fatalf("audit IP: %s", audit.raw)
	}
}

// Оқиғалар журналы орнатуды қысқа идентификаторы бойынша табады.
func TestEventLogFindsAnInstallationByItsShortID(t *testing.T) {
	h := newHarness(t)
	id := "3f2a9c1e-5b7d-4e8f-9a0b-1c2d3e4f5a6b"
	body := map[string]any{"installation_id": id, "events": []any{
		map[string]any{"name": "app_opened", "occurred_at": time.Now().UTC().Format(time.RFC3339), "properties": map[string]any{"cold_start": true}},
	}}
	if res := h.do(http.MethodPost, "/api/v1/events", body, nil); res.status != http.StatusAccepted {
		t.Fatalf("events: %d %s", res.status, res.raw)
	}
	h.telemetry.Flush(context.Background())
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	if res := h.do(http.MethodGet, "/api/v1/admin/logs/events?installation_id="+id[:8], nil, headers); res.num("total") != 1 {
		t.Fatalf("short id: %s", res.raw)
	}
	if res := h.do(http.MethodGet, "/api/v1/admin/logs/events?installation_id=3f2a*", nil, headers); res.status != http.StatusBadRequest {
		t.Fatalf("a pattern is refused: %d", res.status)
	}
}

func viewerSession(t *testing.T, h *harness) map[string]string {
	t.Helper()
	hash, err := auth.HashPassword("viewer-password-long")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.CreateAdmin(context.Background(), domain.AdminUser{
		Email: "viewer@aireply.test", Name: "Viewer", PasswordHash: hash, Role: "viewer",
	}); err != nil {
		t.Fatal(err)
	}
	return h.signInAdminAs("viewer@aireply.test", "viewer-password-long").headers(h.cfg.Admin.CookieName)
}

// Viewer рөлі жеке деректерді біртіндеп таба алмайды: дәл іздеу, қатаң бүркеме, жеке тарих жабық.
func TestViewerCannotRecoverPersonalData(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+77011234567")
	h.mustRegister(installation(installID(85), "android", fcmToken(85)), s.access)
	viewer := viewerSession(t, h)

	if res := h.do(http.MethodGet, "/api/v1/admin/users?q=%2B770112", nil, viewer); res.num("total") != 0 {
		t.Fatalf("a substring search found the user: %s", res.raw)
	}
	exact := h.do(http.MethodGet, "/api/v1/admin/users?q=%2B77011234567", nil, viewer)
	if exact.num("total") != 1 || !strings.Contains(string(exact.raw), `"identifier":"+7******4567"`) {
		t.Fatalf("exact search / strict mask: %s", exact.raw)
	}
	admin := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	if res := h.do(http.MethodGet, "/api/v1/admin/users?q=%2B770112", nil, admin); res.num("total") != 1 {
		t.Fatalf("full admins keep substring search: %s", res.raw)
	}

	for _, path := range []string{"/api/v1/admin/notifications/devices?user_id=" + s.userID,
		"/api/v1/admin/notifications/deliveries?user_id=" + s.userID} {
		if res := h.do(http.MethodGet, path, nil, viewer); res.status != http.StatusForbidden ||
			res.str("error", "details", "permission") != "users.diagnostics.read" {
			t.Fatalf("%s: %d %s", path, res.status, res.raw)
		}
	}
	devices := h.do(http.MethodGet, "/api/v1/admin/notifications/devices", nil, viewer)
	if strings.Contains(string(devices.raw), s.userID) || !strings.Contains(string(devices.raw), `"attached":true`) {
		t.Fatalf("viewer device rows: %s", devices.raw)
	}
	h.do(http.MethodPost, "/api/v1/me", map[string]any{"password": "x"}, h.auth(s.access)) // one recorded API error
	h.telemetry.Flush(context.Background())
	if res := h.do(http.MethodGet, "/api/v1/admin/ops", nil, viewer); strings.Contains(string(res.raw), s.userID) {
		t.Fatalf("ops shows error records to a viewer: %s", res.raw)
	}
	if res := h.do(http.MethodGet, "/api/v1/admin/ops", nil, admin); !strings.Contains(string(res.raw), s.userID) {
		t.Fatalf("ops recent errors for admins: %s", res.raw)
	}
}

// Диагностиканы ашу (аудитке жазылады) CSRF токенсіз мүмкін емес.
func TestDiagnosticsNeedTheCSRFToken(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("csrf-diag@example.com")
	admin := h.signInAdmin()
	cookieOnly := map[string]string{"Cookie": h.cfg.Admin.CookieName + "=" + admin.cookie}
	if res := h.do(http.MethodGet, "/api/v1/admin/users/"+s.userID+"/diagnostics", nil, cookieOnly); res.status != http.StatusForbidden {
		t.Fatalf("diagnostics without the token: %d", res.status)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action = 'user.diagnostics.view'`); n != 0 {
		t.Fatal("a refused request left an audit record")
	}
	if res := h.do(http.MethodGet, "/api/v1/admin/users/"+s.userID+"/diagnostics", nil, admin.headers(h.cfg.Admin.CookieName)); res.status != http.StatusOK {
		t.Fatalf("diagnostics with the token: %d", res.status)
	}
}

// Сақталған жоба сол кілтпен жіберілсе — аудитте send бар.
func TestSendingASavedDraftByRetryIsAudited(t *testing.T) {
	h := newHarness(t)
	admin := h.signInAdmin()
	headers := withKey(admin.headers(h.cfg.Admin.CookieName), "draft-then-send-001")
	body := map[string]any{"title": "Жоба", "body": "Мәтін"}
	if res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", body, headers); res.status != http.StatusCreated {
		t.Fatalf("draft: %d %s", res.status, res.raw)
	}
	body["send"] = true
	sent := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", body, headers)
	if sent.status != http.StatusOK || sent.str("status") != "queued" || sent.body["created"] != false {
		t.Fatalf("send by retry: %d %s", sent.status, sent.raw)
	}
	for action, want := range map[string]int{"notification.campaign.create": 1, "notification.campaign.send": 1} {
		if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action = ?`, action); n != want {
			t.Errorf("%s = %d, want %d", action, n, want)
		}
	}
	again := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", body, headers)
	if again.status != http.StatusOK {
		t.Fatalf("second retry: %d", again.status)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action = 'notification.campaign.send'`); n != 1 {
		t.Fatal("a retry of an already queued campaign is not another send")
	}
}

// Құрылғы іздеуі орнату идентификаторын тек қысқа префиксі бойынша табады; алдын ала қарау шектелген.
func TestInstallationIDCannotBeRecoveredBySearch(t *testing.T) {
	h := newHarness(t)
	h.mustRegister(installation(installID(86), "ios", apnsToken(86)), "")
	admin := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	id := installID(86)
	if res := h.do(http.MethodGet, "/api/v1/admin/notifications/devices?q="+id[:8], nil, admin); res.num("total") != 1 {
		t.Fatalf("short prefix: %s", res.raw)
	}
	if res := h.do(http.MethodGet, "/api/v1/admin/notifications/devices?q="+id[:12], nil, admin); res.num("total") != 0 {
		t.Fatalf("a longer prefix must not match the installation id: %s", res.raw)
	}
	if res := h.do(http.MethodGet, "/api/v1/admin/notifications/devices?q=%25", nil, admin); res.num("total") != 0 {
		t.Fatalf("LIKE wildcards are literal: %s", res.raw)
	}
	if res := h.do(http.MethodGet, "/api/v1/admin/logs/events?installation_id="+id[:12], nil, admin); res.status != http.StatusBadRequest {
		t.Fatalf("event log prefix longer than shown: %d", res.status)
	}
	var last response
	for i := 0; i < 31; i++ {
		last = h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview", map[string]any{"audience": map[string]any{}}, admin)
	}
	if last.status != http.StatusTooManyRequests {
		t.Fatalf("31st preview in a minute: %d", last.status)
	}
}
