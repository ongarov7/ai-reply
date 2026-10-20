package apptest

import (
	"net/http"
	"testing"
	"time"
)

// diagnosticsBody — iOS-тың әдеттегі есебі (тек enum мен уақыт).
func diagnosticsBody() map[string]any {
	return map[string]any{
		"keyboard_setup_status": "not_verified", "keyboard_selected_status": "unsupported",
		"full_access_status": "not_verified", "clipboard_status": "unknown", "onboarding_status": "in_progress",
		"status_source": "app_check", "last_successful_paste_at": nil, "last_keyboard_check_at": nil,
	}
}

func (h *harness) reportDiagnostics(installationID string, body map[string]any, access string) response {
	h.t.Helper()
	headers := map[string]string{}
	if access != "" {
		headers = h.auth(access)
	}
	return h.do(http.MethodPut, "/api/v1/installations/"+installationID+"/diagnostics", body, headers)
}

// Диагностика сақталады және қайтарылады; хабарлама рұқсаты орнатудан шығарылады.
func TestPermissionDiagnosticsAreStoredAndEchoed(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("diag@example.com")
	id := installID(901)
	h.mustRegister(installation(id, "ios", fcmToken(901)), s.access)
	h.pushClock.Advance(time.Hour)

	body := diagnosticsBody()
	body["keyboard_setup_status"] = "verified_enabled"
	body["last_keyboard_check_at"] = h.pushClock.Now().Add(-time.Minute).Format(time.RFC3339)
	res := h.reportDiagnostics(id, body, s.access)
	if res.status != http.StatusOK {
		t.Fatalf("report: %d %s", res.status, res.raw)
	}
	for key, want := range map[string]string{
		"keyboard_setup_status": "verified_enabled", "keyboard_selected_status": "unsupported",
		"full_access_status": "not_verified", "clipboard_status": "unknown", "onboarding_status": "in_progress",
		"status_source": "app_check", "notification_authorization_status": "verified_enabled",
		"last_keyboard_check_at": h.pushClock.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
		"updated_at":             h.pushClock.Now().UTC().Format(time.RFC3339),
	} {
		if got := res.str(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if v, ok := res.body["last_successful_paste_at"]; !ok || v != nil {
		t.Errorf("last_successful_paste_at = %v (present %v), want null", v, ok)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM installation_diagnostics WHERE keyboard_verified_at IS NOT NULL`); n != 1 {
		t.Fatalf("verified rows = %d", n)
	}

	// The verification is sticky: a later ambiguous check keeps it.
	h.pushClock.Advance(time.Hour)
	if res := h.reportDiagnostics(id, diagnosticsBody(), s.access); res.status != http.StatusOK ||
		res.str("keyboard_setup_status") != "not_verified" {
		t.Fatalf("second report: %d %s", res.status, res.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM installation_diagnostics WHERE keyboard_verified_at IS NOT NULL`); n != 1 {
		t.Fatal("keyboard_verified_at was cleared")
	}

	// The in-app switch off shows as user_reported_disabled.
	off := installation(id, "ios", fcmToken(901))
	off["notifications_enabled"] = false
	h.mustRegister(off, s.access)
	if res := h.reportDiagnostics(id, diagnosticsBody(), s.access); res.str("notification_authorization_status") != "user_reported_disabled" {
		t.Fatalf("notification status = %s", res.raw)
	}
}

// Қатаң дене: белгісіз өріс, белгісіз мән, қате уақыт — 400.
func TestPermissionDiagnosticsValidation(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("diag-strict@example.com")
	id := installID(902)
	h.mustRegister(installation(id, "android", fcmToken(902)), s.access)

	cases := map[string]func(map[string]any){
		"unknown field":   func(b map[string]any) { b["clipboard_text"] = "secret" },
		"bad status":      func(b map[string]any) { b["clipboard_status"] = "granted" },
		"bad onboarding":  func(b map[string]any) { b["onboarding_status"] = "verified_enabled" },
		"bad source":      func(b map[string]any) { b["status_source"] = "server_inferred" },
		"missing source":  func(b map[string]any) { delete(b, "status_source") },
		"bad timestamp":   func(b map[string]any) { b["last_successful_paste_at"] = "yesterday" },
		"number for time": func(b map[string]any) { b["last_keyboard_check_at"] = 1700000000 },
	}
	for name, mutate := range cases {
		body := diagnosticsBody()
		mutate(body)
		if res := h.reportDiagnostics(id, body, s.access); res.status != http.StatusBadRequest {
			t.Errorf("%s: status %d %s", name, res.status, res.raw)
		}
	}
	if res := h.reportDiagnostics("bad", diagnosticsBody(), s.access); res.status != http.StatusBadRequest {
		t.Errorf("bad installation id: %d", res.status)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM installation_diagnostics`); n != 0 {
		t.Fatalf("rows after refused reports = %d", n)
	}
}

// Орнату болуы керек және басқа тіркелгіге тиесілі болмауы керек.
func TestPermissionDiagnosticsOwnership(t *testing.T) {
	h := newHarness(t)
	owner := h.signIn("diag-owner@example.com")
	other := h.signIn("diag-other@example.com")
	id := installID(903)
	h.mustRegister(installation(id, "ios", fcmToken(903)), owner.access)

	if res := h.reportDiagnostics(installID(904), diagnosticsBody(), owner.access); res.status != http.StatusNotFound {
		t.Errorf("unregistered installation: %d", res.status)
	}
	if res := h.reportDiagnostics(id, diagnosticsBody(), other.access); res.status != http.StatusConflict {
		t.Errorf("another account: %d %s", res.status, res.raw)
	}
	if res := h.reportDiagnostics(id, diagnosticsBody(), ""); res.status != http.StatusConflict {
		t.Errorf("anonymous caller on an attached installation: %d", res.status)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM installation_diagnostics`); n != 0 {
		t.Fatalf("rows = %d", n)
	}
	// An anonymous installation reports anonymously.
	anon := installID(905)
	h.mustRegister(installation(anon, "android", ""), "")
	if res := h.reportDiagnostics(anon, diagnosticsBody(), ""); res.status != http.StatusOK {
		t.Fatalf("anonymous installation: %d %s", res.status, res.raw)
	}
}

// Уақыттар [алғаш көрінген, қазір + 5 мин] аралығына қысылады.
func TestPermissionDiagnosticsClampTimestamps(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("diag-clamp@example.com")
	id := installID(906)
	firstSeen := h.pushClock.Now()
	h.mustRegister(installation(id, "ios", fcmToken(906)), s.access)
	h.pushClock.Advance(2 * time.Hour)

	body := diagnosticsBody()
	body["last_successful_paste_at"] = "2001-01-01T00:00:00Z"
	body["last_keyboard_check_at"] = h.pushClock.Now().Add(72 * time.Hour).Format(time.RFC3339)
	res := h.reportDiagnostics(id, body, s.access)
	if res.status != http.StatusOK {
		t.Fatalf("report: %d %s", res.status, res.raw)
	}
	if got := res.str("last_successful_paste_at"); got != firstSeen.UTC().Format(time.RFC3339) {
		t.Errorf("paste = %s, want the first sighting", got)
	}
	if got := res.str("last_keyboard_check_at"); got != h.pushClock.Now().Add(5*time.Minute).UTC().Format(time.RFC3339) {
		t.Errorf("check = %s, want now + 5 min", got)
	}
}

// Тіркелгі жойылғанда құрылғылардың диагностикасы да жойылады (орнату анонимді қалады).
func TestAccountDeletionRemovesDiagnostics(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("diag-delete@example.com")
	id := installID(907)
	h.mustRegister(installation(id, "ios", fcmToken(907)), s.access)
	if res := h.reportDiagnostics(id, diagnosticsBody(), s.access); res.status != http.StatusOK {
		t.Fatalf("report: %d", res.status)
	}
	if res := h.do(http.MethodDelete, "/api/v1/me", nil, h.auth(s.access)); res.status != http.StatusOK {
		t.Fatalf("delete: %d %s", res.status, res.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM installation_diagnostics`); n != 0 {
		t.Fatalf("diagnostics after deletion = %d", n)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM app_installations WHERE installation_id = ?`, id); n != 1 {
		t.Fatalf("installation rows = %d", n)
	}
}

// /api/v1/config: permission_diagnostics қосулы, push_reminders — әкімші баптауы.
func TestDiagnosticsFeatureFlags(t *testing.T) {
	h := newHarness(t)
	features := func() map[string]any {
		res := h.do(http.MethodGet, "/api/v1/config", nil, nil)
		f, _ := res.body["features"].(map[string]any)
		return f
	}
	if f := features(); f["permission_diagnostics"] != true || f["push_reminders"] != false {
		t.Fatalf("features = %v", f)
	}
	h.enableReminders()
	if f := features(); f["push_reminders"] != true {
		t.Fatalf("push_reminders after enabling = %v", f["push_reminders"])
	}
}
