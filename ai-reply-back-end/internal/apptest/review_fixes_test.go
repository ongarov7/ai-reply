package apptest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
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
