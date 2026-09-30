package apptest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
)

func appHeaders(installation string, extra map[string]string) map[string]string {
	out := map[string]string{
		"X-Platform": "android", "X-App-Version": "1.3.2", "X-App-Build": "142", "X-OS-Version": "15",
		"X-Installation-ID": installation,
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func event(name string, props map[string]any) map[string]any {
	return map[string]any{"name": name, "occurred_at": time.Now().UTC().Format(time.RFC3339), "properties": props}
}

// Оқиғалар: тек рұқсат етілгендері, тіркелгімен байланысады, сессия ашылады.
func TestEventsAreValidatedLinkedAndBatched(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("events@example.com")
	h.mustRegister(installation(installID(60), "android", fcmToken(60)), s.access)
	session := "5e55a0b1-0000-4000-8000-000000000060"

	res := h.do(http.MethodPost, "/api/v1/events", map[string]any{
		"installation_id": installID(60), "session_id": session,
		"events": []any{
			event("app_opened", map[string]any{"cold_start": true}),
			event("push_permission_granted", map[string]any{"status": "authorized"}),
			event("keyboard_key_pressed", map[string]any{"key": "a"}),
			event("login_failed", map[string]any{"method": "email", "message": "secret text"}),
		},
	}, appHeaders(installID(60), h.auth(s.access)))
	if res.status != http.StatusAccepted || res.num("accepted") != 2 || res.num("rejected") != 2 {
		t.Fatalf("events: %d %s", res.status, res.raw)
	}
	h.telemetry.Flush(context.Background())

	if n := h.scalar(`SELECT COUNT(*) FROM app_events WHERE user_id = ? AND installation_id = ? AND app_build = '142' AND device_model = 'SM-S928B'`,
		s.userID, installID(60)); n != 2 {
		t.Fatalf("stored events = %d", n)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM app_events WHERE event_name = 'keyboard_key_pressed' OR properties LIKE '%secret text%'`); n != 0 {
		t.Fatal("a refused event reached the database")
	}
	if n := h.scalar(`SELECT event_count FROM app_sessions WHERE session_id = ? AND user_id = ?`, session, s.userID); n != 2 {
		t.Fatalf("session events = %d", n)
	}
}

// Шектер: топ өлшемі, дене өлшемі, қате пішім.
func TestEventLimits(t *testing.T) {
	h := newHarness(t)
	many := make([]any, 51)
	for i := range many {
		many[i] = event("app_opened", nil)
	}
	if res := h.do(http.MethodPost, "/api/v1/events", map[string]any{"installation_id": installID(61), "events": many}, nil); res.status != http.StatusBadRequest {
		t.Fatalf("51 events: %d", res.status)
	}
	huge := map[string]any{"installation_id": installID(61), "events": []any{
		event("app_opened", map[string]any{"cold_start": strings.Repeat("x", 70*1024)}),
	}}
	if res := h.do(http.MethodPost, "/api/v1/events", huge, nil); res.status != http.StatusBadRequest {
		t.Fatalf("oversized body: %d", res.status)
	}
	if res := h.do(http.MethodPost, "/api/v1/events", map[string]any{"installation_id": installID(61), "events": []any{}, "extra": 1}, nil); res.status != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", res.status)
	}
}

// Қосымша «ашылды» дегенде жеткізу ашылған болып белгіленеді (тек сол құрылғыдан).
func TestNotificationOpenedIsRecordedFromTheReceivingDevice(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("opener@example.com")
	h.mustRegister(installation(installID(62), "ios", apnsToken(62)), s.access)
	res := h.notifyUser(s.userID, "open:1")
	h.tick()
	did := h.text(`SELECT id FROM notification_deliveries WHERE notification_id = ?`, res.NotificationID)

	opened := event("notification_opened", map[string]any{"notification_id": res.NotificationID, "delivery_id": did})
	// Another installation cannot mark it.
	h.do(http.MethodPost, "/api/v1/events", map[string]any{"installation_id": installID(63), "events": []any{opened}}, nil)
	h.telemetry.Flush(context.Background())
	if n := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE id = ? AND opened_at IS NOT NULL`, did); n != 0 {
		t.Fatal("another device marked the delivery as opened")
	}
	h.do(http.MethodPost, "/api/v1/events", map[string]any{"installation_id": installID(62), "events": []any{opened}}, h.auth(s.access))
	h.telemetry.Flush(context.Background())
	if n := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE id = ? AND opened_at IS NOT NULL`, did); n != 1 {
		t.Fatal("the open was not recorded")
	}
}

// Кіру оқиғалары жазылады: пошта мен код сақталмайды, хэш бойынша табылады.
func TestAuthEventsAreRecordedWithoutSecrets(t *testing.T) {
	h := newHarness(t)
	email := "Auth-Events@Example.com"
	h.do(http.MethodPost, "/api/v1/auth/email/otp/request", map[string]any{"email": email, "locale": "ru"},
		appHeaders(installID(64), nil))
	bad := h.do(http.MethodPost, "/api/v1/auth/email/otp/verify", map[string]any{
		"email": email, "code": "9999", "device": map[string]any{"platform": "android", "app_version": "1.3.2"},
	}, appHeaders(installID(64), nil))
	if bad.status == http.StatusOK {
		t.Fatal("wrong code accepted")
	}
	good := h.do(http.MethodPost, "/api/v1/auth/email/otp/verify", map[string]any{
		"email": email, "code": "1111", "device": map[string]any{"platform": "android", "app_version": "1.3.2"},
	}, appHeaders(installID(64), nil))
	if good.status != http.StatusOK {
		t.Fatalf("sign-in: %d %s", good.status, good.raw)
	}
	userID := good.str("user", "id")
	names := map[string]int{}
	rows, err := h.db.Reader().Query(`SELECT event_name, outcome FROM auth_events`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name, outcome string
		_ = rows.Scan(&name, &outcome)
		names[name+"/"+outcome]++
	}
	rows.Close()
	for key, want := range map[string]int{
		"otp_requested/success": 1, "otp_failed/failure": 1, "auth_login_failed/failure": 1,
		"otp_verified/success": 1, "auth_signup_success/success": 1,
	} {
		if names[key] != want {
			t.Errorf("%s = %d (%v)", key, names[key], names)
		}
	}
	if n := h.scalar(`SELECT COUNT(*) FROM auth_events WHERE installation_id = ? AND platform = 'android' AND ip <> ''`, installID(64)); n != 5 {
		t.Fatalf("auth events with context = %d", n)
	}

	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	logs := h.do(http.MethodGet, "/api/v1/admin/logs/auth?user=auth-events@example.com", nil, headers)
	if logs.num("total") != 5 {
		t.Fatalf("search by e-mail finds the failed attempt too: %s", logs.raw)
	}
	raw := string(logs.raw)
	if strings.Contains(raw, "9999") || strings.Contains(strings.ToLower(raw), "auth-events@example.com") {
		t.Fatalf("auth log leaks the code or the address: %s", raw)
	}
	byID := h.do(http.MethodGet, "/api/v1/admin/logs/auth?user="+userID[:8], nil, headers)
	if byID.num("total") != 2 {
		t.Fatalf("search by user id prefix finds the attempts linked to the account: %s", byID.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM auth_events WHERE subject_hash = '' OR subject_hash LIKE '%@%'`); n != 0 {
		t.Fatal("every e-mail attempt carries a keyed hash, never the address")
	}
}

// API қатесі: request id қайтарылады, метадерек сақталады, дене сақталмайды.
func TestAPIErrorsCarryTheRequestIDAndAreRecorded(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("errors@example.com")
	headers := appHeaders(installID(65), map[string]string{"X-Request-ID": "req_client_0001", "Authorization": "Bearer " + s.access})
	res := h.do(http.MethodPost, "/api/v1/me", map[string]any{"preferred_tone": "angry", "password": "hunter2"}, headers)
	if res.status != http.StatusBadRequest || res.header.Get("X-Request-ID") != "req_client_0001" ||
		res.str("error", "request_id") != "req_client_0001" {
		t.Fatalf("error response: %d %v %s", res.status, res.header, res.raw)
	}
	// An id outside the safe alphabet is replaced, never echoed or logged.
	for _, bad := range []string{`x" level=error`, "short", strings.Repeat("a", 65)} {
		forged := h.do(http.MethodGet, "/healthz", nil, map[string]string{"X-Request-ID": bad})
		if id := forged.header.Get("X-Request-ID"); !strings.HasPrefix(id, "req_") || id == bad {
			t.Fatalf("unsafe id %q kept as %q", bad, id)
		}
	}
	h.telemetry.Flush(context.Background())
	var route, code, version, user string
	var status int
	if err := h.db.Reader().QueryRow(`SELECT route, error_code, app_version, COALESCE(user_id, ''), status_code FROM api_errors WHERE request_id = ?`,
		"req_client_0001").Scan(&route, &code, &version, &user, &status); err != nil {
		t.Fatalf("api error not recorded: %v", err)
	}
	if route != "/api/v1/me" || code != "INVALID_REQUEST" || version != "1.3.2" || user != s.userID || status != 400 {
		t.Fatalf("recorded: %s %s %s %s %d", route, code, version, user, status)
	}
	if h.dbContains("hunter2") {
		t.Fatal("a request body reached the database")
	}
}

// Журналда токен, код, кілт, пошта жоқ.
func TestLogsNeverContainSecrets(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("logs@example.com")
	token := fcmToken(66)
	h.mustRegister(installation(installID(66), "android", token), s.access)
	h.notifyUser(s.userID, "logs:1")
	h.tick()
	logs := h.logs.String()
	for _, secret := range []string{token, s.access, s.refresh, "logs@example.com", h.cfg.Auth.AccessSecret} {
		if strings.Contains(logs, secret) {
			t.Fatalf("logs contain a secret (%.12s…)", secret)
		}
	}
	if !strings.Contains(logs, `"event":"push_delivery"`) || !strings.Contains(logs, `"push":"fcm:`) {
		t.Fatal("delivery log lines must carry the event name and the token fingerprint")
	}
	if !strings.Contains(logs, `"route":"/api/v1/installations"`) || !strings.Contains(logs, `"user_id":"`+s.userID+`"`) {
		t.Fatal("access log lines must name the route and the user")
	}
}

// Сақтау мерзімі: ескі аналитика өшеді, аудит өшпейді (әдепкі 0 күн).
func TestRetentionRemovesOnlyExpiredRows(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	old := time.Now().AddDate(0, 0, -400).UnixMilli()
	for _, stmt := range []string{
		`INSERT INTO app_events (id, event_name, installation_id, occurred_at, received_at) VALUES ('e-old', 'app_opened', 'i', ?, ?)`,
		`INSERT INTO api_errors (id, method, route, status_code, occurred_at) VALUES ('a-old', 'GET', '/api/v1/x', 500, ?)`,
		`INSERT INTO admin_audit_logs (id, admin_id, action, created_at) VALUES ('audit-old', 'x', 'test', ?)`,
	} {
		args := []any{old}
		if strings.Count(stmt, "?") == 2 {
			args = append(args, old)
		}
		if _, err := h.db.Writer().ExecContext(ctx, stmt, args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.db.Writer().ExecContext(ctx,
		`INSERT INTO app_events (id, event_name, installation_id, occurred_at, received_at) VALUES ('e-new', 'app_opened', 'i', ?, ?)`,
		time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	removed := h.telemetry.RunRetention(ctx)
	if removed["app_events"] != 1 || removed["api_errors"] != 1 {
		t.Fatalf("removed = %v", removed)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE id = 'audit-old'`); n != 1 {
		t.Fatal("audit records are kept unless a retention period is configured")
	}
	if n := h.scalar(`SELECT COUNT(*) FROM app_events`); n != 1 {
		t.Fatal("recent events must stay")
	}
}

// Операциялық тақта және нұсқалар есебі құпиясыз жиынтық береді.
func TestOpsAndVersionReports(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("ops@example.com")
	h.mustRegister(installation(installID(67), "android", fcmToken(67)), s.access)
	h.mustRegister(installation(installID(68), "ios", apnsToken(68)), "")
	h.do(http.MethodPost, "/api/v1/events", map[string]any{"installation_id": installID(67), "events": []any{
		event("push_token_registration_failed", map[string]any{"provider": "fcm", "error_code": "SERVICE_NOT_AVAILABLE"}),
	}}, appHeaders(installID(67), nil))
	h.telemetry.Flush(context.Background())

	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	ops := h.do(http.MethodGet, "/api/v1/admin/ops", nil, headers)
	if ops.num("summary", "installations") != 2 || ops.num("summary", "fcm_active") != 1 || ops.num("summary", "apns_active") != 1 {
		t.Fatalf("ops: %s", ops.raw)
	}
	versions := h.do(http.MethodGet, "/api/v1/admin/logs/versions", nil, headers)
	if !strings.Contains(string(versions.raw), `"push_registration_failures_7d":1`) {
		t.Fatalf("versions: %s", versions.raw)
	}
	failures := h.do(http.MethodGet, "/api/v1/admin/logs/events?name=push_token_registration_failed&platform=android&app_build=142", nil, headers)
	if failures.num("total") != 1 {
		t.Fatalf("event filter: %s", failures.raw)
	}
}

// Бір notifications.Service-ті қолданатын бизнес-код (NotifyUser) push өшірулі болса ештеңе жасамайды.
func TestNotifyUserDoesNothingWhenPushIsOff(t *testing.T) {
	h := newHarness(t, withEnv("PUSH_NOTIFICATIONS_ENABLED", "false"))
	s := h.signIn("push-off@example.com")
	h.mustRegister(installation(installID(69), "android", fcmToken(69)), s.access)
	res, err := h.notify.NotifyUser(context.Background(), notifications.UserNotification{
		UserID: s.userID, IdempotencyKey: "off:1", Type: "notice", Category: domain.CategorySystem, Title: "t", Body: "b",
	})
	if err != nil || res.Skipped != "push_disabled" {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	cfg := h.do(http.MethodGet, "/api/v1/config", nil, nil)
	features := cfg.body["features"].(map[string]any)
	if features["push_notifications"] != false || features["installations"] != true {
		t.Fatalf("features: %v", features)
	}
	admin := h.signInAdmin()
	send := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", map[string]any{"title": "t", "body": "b", "send": true},
		withKey(admin.headers(h.cfg.Admin.CookieName), "push-off-campaign-01"))
	if send.status != http.StatusConflict || send.errorCode() != "PUSH_DISABLED" {
		t.Fatalf("send with push off: %d %s", send.status, send.raw)
	}
}

// Әкімші әрекеттері аудитке сұраныс идентификаторымен жазылады; сүзгілер жұмыс істейді.
func TestAdminActionsAreAuditedWithTheRequestID(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("audited@example.com")
	h.mustRegister(installation(installID(70), "android", fcmToken(70)), s.access)
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)

	create := withKey(headers, "audit-campaign-0001")
	create["X-Request-ID"] = "req_audit_create_01"
	res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns",
		map[string]any{"title": "Жаңалық", "body": "Жаңа нұсқа шықты", "send": true}, create)
	if res.status != http.StatusCreated {
		t.Fatalf("campaign: %d %s", res.status, res.raw)
	}
	diag := h.do(http.MethodGet, "/api/v1/admin/users/"+s.userID+"/diagnostics", nil, headers)
	if diag.status != http.StatusOK || strings.Contains(string(diag.raw), fcmToken(70)) {
		t.Fatalf("diagnostics: %d %s", diag.status, diag.raw)
	}

	audit := h.do(http.MethodGet, "/api/v1/admin/audit?action=notification.campaign.", nil, headers)
	entries, _ := audit.body["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("campaign audit entries = %d: %s", len(entries), audit.raw)
	}
	for _, raw := range entries {
		entry := raw.(map[string]any)
		if entry["request_id"] != "req_audit_create_01" {
			t.Fatalf("audit entry without the request id: %v", entry)
		}
	}
	viewed := h.do(http.MethodGet, "/api/v1/admin/audit?action=user.diagnostics.view&entity_id="+s.userID, nil, headers)
	if viewed.num("total") != 1 {
		t.Fatalf("diagnostics views are audited: %s", viewed.raw)
	}
	byAdmin := h.do(http.MethodGet, "/api/v1/admin/audit?admin=nobody@example.com", nil, headers)
	if byAdmin.num("total") != 0 {
		t.Fatalf("admin filter: %s", byAdmin.raw)
	}
}
