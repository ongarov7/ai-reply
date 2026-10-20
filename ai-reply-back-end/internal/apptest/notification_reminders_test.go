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

// enableReminders — еске салуларды қосады (әдепкі үзілістер мен шектермен).
func (h *harness) enableReminders() {
	h.t.Helper()
	set := notifications.DefaultReminderSettings()
	set.Enabled = true
	if _, err := h.notify.UpdateReminderSettings(context.Background(), set); err != nil {
		h.t.Fatalf("reminder settings: %v", err)
	}
}

// keyboardBody — тек пернетақта дәлелі: онбординг аяқталған, буфер белгісіз.
func keyboardBody() map[string]any {
	body := diagnosticsBody()
	body["onboarding_status"] = "completed"
	return body
}

// agedSignIn — кірген қолданушы, тіркелгісі еске салудың ең аз жасынан үлкен (pushClock бойынша).
func (h *harness) agedSignIn(email string) session {
	h.t.Helper()
	s := h.signIn(email)
	if _, err := h.db.Writer().Exec(`UPDATE users SET created_at = ? WHERE id = ?`,
		h.pushClock.Now().Add(-48*time.Hour).UnixMilli(), s.userID); err != nil {
		h.t.Fatal(err)
	}
	return s
}

// withDiagnostics — кірген қолданушы, iOS орнатуы және оның диагностикасы.
func (h *harness) withDiagnostics(email string, seed int, body map[string]any) session {
	h.t.Helper()
	s := h.agedSignIn(email)
	h.mustRegister(installation(installID(seed), "ios", fcmToken(seed)), s.access)
	if res := h.reportDiagnostics(installID(seed), body, s.access); res.status != http.StatusOK {
		h.t.Fatalf("diagnostics: %d %s", res.status, res.raw)
	}
	return s
}

func (h *harness) reminders(userID string) int {
	h.t.Helper()
	return h.scalar(`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND type IN
		('onboarding_incomplete','keyboard_setup_incomplete','clipboard_help','account_info')`, userID)
}

// Тек жаңа диагностика дәлел: жоқ, белгісіз, ескі, қолданушы сөзі және жас тіркелгі — ештеңе.
func TestRemindersNeedFreshEvidence(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.pushClock.Advance(25 * time.Hour)
	fresh := h.withDiagnostics("fresh@example.com", 911, keyboardBody())

	h.events.SweepReminders(ctx)
	if n := h.reminders(fresh.userID); n != 0 {
		t.Fatalf("reminders are off by default, sent %d", n)
	}
	h.enableReminders()

	none := h.agedSignIn("none@example.com")
	h.mustRegister(installation(installID(912), "ios", fcmToken(912)), none.access)
	unknown := diagnosticsBody()
	for _, k := range []string{"keyboard_setup_status", "full_access_status", "clipboard_status", "onboarding_status"} {
		unknown[k] = "unknown"
	}
	vague := h.withDiagnostics("unknown@example.com", 913, unknown)
	userReport := keyboardBody()
	userReport["status_source"] = "user_report"
	said := h.withDiagnostics("said@example.com", 914, userReport)
	stale := h.withDiagnostics("stale@example.com", 915, keyboardBody())
	if _, err := h.db.Writer().Exec(`UPDATE installation_diagnostics SET reported_at = ? WHERE installation_id =
		(SELECT id FROM app_installations WHERE installation_id = ?)`,
		h.pushClock.Now().Add(-15*24*time.Hour).UnixMilli(), installID(915)); err != nil {
		t.Fatal(err)
	}
	young := h.withDiagnostics("young@example.com", 916, keyboardBody())
	if _, err := h.db.Writer().Exec(`UPDATE users SET created_at = ? WHERE id = ?`,
		h.pushClock.Now().Add(-time.Hour).UnixMilli(), young.userID); err != nil {
		t.Fatal(err)
	}

	h.events.SweepReminders(ctx)
	for name, s := range map[string]session{"none": none, "unknown": vague, "user report": said, "stale": stale, "young": young} {
		if n := h.reminders(s.userID); n != 0 {
			t.Errorf("%s: %d reminders without evidence", name, n)
		}
	}
	if n := h.notices(fresh.userID, domain.TypeKeyboardSetupIncomplete); n != 1 {
		t.Fatalf("fresh evidence: %d reminders", n)
	}
	if n := h.reminders(fresh.userID); n != 1 {
		t.Fatalf("other reminder types for keyboard-only evidence: %d", n)
	}
}

// Үзіліс, ең көп саны, қайта шолу және екінші процесс: әр әрекет бір-ақ рет.
func TestKeyboardReminderCooldownAndMaxSends(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.enableReminders()
	h.pushClock.Advance(25 * time.Hour)
	s := h.withDiagnostics("keyboard@example.com", 921, keyboardBody())

	second := notifications.NewEvents(h.notify)
	h.events.SweepReminders(ctx)
	h.events.SweepReminders(ctx)
	second.SweepReminders(ctx)
	if keys := h.noticeKeys(s.userID); len(keys) != 1 || keys[0] != "keyboard_setup_incomplete:1" {
		t.Fatalf("keys = %v", keys)
	}
	h.tick()
	msgs := h.pushesOf(domain.TypeKeyboardSetupIncomplete)
	if len(msgs) != 1 || msgs[0].Title != "Пернетақта әлі бапталмаған" || msgs[0].Data["link"] != "aireply://keyboard" ||
		msgs[0].Category != domain.CategorySystem || msgs[0].TTL != notifications.ReminderTTL {
		t.Fatalf("reminder push = %+v", msgs)
	}

	h.pushClock.Advance(47 * time.Hour)
	h.events.SweepReminders(ctx)
	if n := h.notices(s.userID, domain.TypeKeyboardSetupIncomplete); n != 1 {
		t.Fatalf("inside the cooldown: %d", n)
	}
	for want := 2; want <= 3; want++ {
		h.pushClock.Advance(49 * time.Hour)
		h.events.SweepReminders(ctx)
		second.SweepReminders(ctx)
		if n := h.notices(s.userID, domain.TypeKeyboardSetupIncomplete); n != want {
			t.Fatalf("after cooldown %d: %d", want, n)
		}
	}
	h.pushClock.Advance(49 * time.Hour)
	h.events.SweepReminders(ctx)
	if keys := strings.Join(h.noticeKeys(s.userID), ","); keys !=
		"keyboard_setup_incomplete:1,keyboard_setup_incomplete:2,keyboard_setup_incomplete:3" {
		t.Fatalf("max sends: %s", keys)
	}
	if n := h.scalar(`SELECT send_count FROM notification_reminders WHERE user_id = ? AND type = ?`,
		s.userID, domain.TypeKeyboardSetupIncomplete); n != 3 {
		t.Fatalf("ledger = %d", n)
	}
}

// Расталған сәт еске салуды біржола тоқтатады (пернетақта және буфер).
func TestVerifiedSuccessResolvesReminders(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.enableReminders()
	h.pushClock.Advance(25 * time.Hour)
	denied := keyboardBody()
	denied["clipboard_status"] = "observed_denied"
	s := h.withDiagnostics("resolve@example.com", 931, denied)

	h.events.SweepReminders(ctx)
	if h.notices(s.userID, domain.TypeKeyboardSetupIncomplete) != 1 || h.notices(s.userID, domain.TypeClipboardHelp) != 1 {
		t.Fatalf("keys = %v", h.noticeKeys(s.userID))
	}
	verified := keyboardBody()
	verified["keyboard_setup_status"] = "verified_enabled"
	verified["clipboard_status"] = "verified_enabled"
	verified["last_successful_paste_at"] = h.pushClock.Now().Format(time.RFC3339)
	if res := h.reportDiagnostics(installID(931), verified, s.access); res.status != http.StatusOK {
		t.Fatalf("verified: %d", res.status)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notification_reminders WHERE user_id = ? AND resolved_at IS NOT NULL
		AND type IN ('keyboard_setup_incomplete','clipboard_help')`, s.userID); n != 2 {
		t.Fatalf("resolved = %d", n)
	}
	// Ambiguous again later: the reminder stays resolved.
	h.pushClock.Advance(200 * time.Hour)
	if res := h.reportDiagnostics(installID(931), denied, s.access); res.status != http.StatusOK {
		t.Fatalf("report: %d", res.status)
	}
	h.events.SweepReminders(ctx)
	if n := h.reminders(s.userID); n != 2 {
		t.Fatalf("reminders after a verified success = %d (%v)", n, h.noticeKeys(s.userID))
	}
}

// Жеткізуге құрылғы жоқ тіркелгі әрекетті жұмсамайды.
func TestUnreachableAccountUsesNoReminderAttempt(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.enableReminders()
	h.pushClock.Advance(25 * time.Hour)
	s := h.agedSignIn("unreachable@example.com")
	blocked := installation(installID(941), "ios", fcmToken(941))
	blocked["notification_permission"] = "denied"
	h.mustRegister(blocked, s.access)
	if res := h.reportDiagnostics(installID(941), keyboardBody(), s.access); res.status != http.StatusOK {
		t.Fatalf("report: %d", res.status)
	}
	h.events.SweepReminders(ctx)
	if n := h.scalar(`SELECT COUNT(*) FROM notifications WHERE user_id = ?`, s.userID); n != 0 {
		t.Fatalf("notifications = %d", n)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notification_reminders WHERE user_id = ? AND send_count > 0`, s.userID); n != 0 {
		t.Fatalf("ledger rows = %d", n)
	}
	h.mustRegister(installation(installID(941), "ios", fcmToken(941)), s.access)
	h.events.SweepReminders(ctx)
	if keys := h.noticeKeys(s.userID); len(keys) != 1 || keys[0] != "keyboard_setup_incomplete:1" {
		t.Fatalf("keys = %v", keys)
	}
}

// Тыныш сағатта (алушының жергілікті уақыты) еске салу кейінгі шолуды күтеді.
func TestRemindersWaitForQuietHours(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.enableReminders()
	h.pushClock.T = time.Date(2026, 3, 11, 18, 0, 0, 0, time.UTC) // 23:00 in Almaty
	s := h.withDiagnostics("night@example.com", 951, keyboardBody())
	h.events.SweepReminders(ctx)
	if n := h.reminders(s.userID); n != 0 {
		t.Fatalf("sent in quiet hours: %d", n)
	}
	h.pushClock.Advance(11 * time.Hour) // 10:00 in Almaty
	h.events.SweepReminders(ctx)
	if n := h.reminders(s.userID); n != 1 {
		t.Fatalf("after quiet hours: %d", n)
	}
}

// Әкімшінің еске салуы: науқан еске салу түрімен, журналға жазылады, аудитте, тарих түрі бойынша.
func TestAdminReminderCampaign(t *testing.T) {
	h := newHarness(t)
	s := h.withDiagnostics("admin-remind@example.com", 961, keyboardBody())
	h.withDiagnostics("admin-done@example.com", 962, func() map[string]any {
		b := keyboardBody()
		b["keyboard_setup_status"] = "verified_enabled"
		return b
	}())
	adminSession := h.signInAdmin()
	headers := func(key string) map[string]string {
		out := adminSession.headers(h.cfg.Admin.CookieName)
		if key != "" {
			out["Idempotency-Key"] = key
		}
		return out
	}
	body := map[string]any{"type": "keyboard_setup_incomplete", "audience": map[string]any{"diagnostics": "keyboard_not_verified"}}

	if res := h.do(http.MethodPost, "/api/v1/admin/notifications/reminders", body, headers("")); res.status != http.StatusBadRequest {
		t.Fatalf("without Idempotency-Key: %d", res.status)
	}
	for name, bad := range map[string]map[string]any{
		"everyone":     {"type": "keyboard_setup_incomplete", "audience": map[string]any{}},
		"unknown type": {"type": "quota_low", "audience": map[string]any{"diagnostics": "keyboard_not_verified"}},
		"bad filter":   {"type": "clipboard_help", "audience": map[string]any{"diagnostics": "everything"}},
	} {
		if res := h.do(http.MethodPost, "/api/v1/admin/notifications/reminders", bad, headers("reminder-"+strings.ReplaceAll(name, " ", "-"))); res.status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, res.status, res.raw)
		}
	}

	res := h.do(http.MethodPost, "/api/v1/admin/notifications/reminders", body, headers("reminder-key-0001"))
	if res.status != http.StatusCreated || res.str("notification_type") != "keyboard_setup_incomplete" || res.str("category") != "system" {
		t.Fatalf("create: %d %s", res.status, res.raw)
	}
	again := h.do(http.MethodPost, "/api/v1/admin/notifications/reminders", body, headers("reminder-key-0001"))
	if again.status != http.StatusOK || again.str("id") != res.str("id") || again.body["created"] != false {
		t.Fatalf("retry: %d %s", again.status, again.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action = 'campaign.create'
		AND metadata LIKE '%"reminder_type":"keyboard_setup_incomplete"%'`); n != 1 {
		t.Fatalf("create audits = %d", n)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action = 'campaign.send'`); n != 1 {
		t.Fatalf("send audits = %d", n)
	}

	h.tick()
	if n := h.scalar(`SELECT COUNT(*) FROM notifications WHERE campaign_id = ? AND type = 'keyboard_setup_incomplete'`, res.str("id")); n != 1 {
		t.Fatalf("campaign notifications of the reminder type = %d", n)
	}
	if msgs := h.pushesOf(domain.TypeKeyboardSetupIncomplete); len(msgs) != 1 || msgs[0].Data["link"] != "aireply://keyboard" {
		t.Fatalf("pushes = %+v", msgs)
	}
	if got := h.text(`SELECT last_source || ':' || send_count FROM notification_reminders WHERE user_id = ? AND type = ?`,
		s.userID, domain.TypeKeyboardSetupIncomplete); got != "admin:1" {
		t.Fatalf("ledger = %s", got)
	}
	list := h.do(http.MethodGet, "/api/v1/admin/notifications/deliveries?type=keyboard_setup_incomplete", nil, headers(""))
	if list.status != http.StatusOK || list.num("total") != 1 {
		t.Fatalf("deliveries by type: %d %s", list.status, list.raw)
	}

	// The automatic sweep keeps the cooldown the admin send started.
	h.enableReminders()
	h.pushClock.Advance(25 * time.Hour)
	h.events.SweepReminders(context.Background())
	if n := h.scalar(`SELECT COUNT(*) FROM notifications WHERE user_id = ?`, s.userID); n != 0 {
		t.Fatalf("automatic reminder inside the admin cooldown: %d", n)
	}
}

// Әкімшінің диагностика тізімі, қолданушы карточкасы және еске салу баптаулары.
func TestAdminDiagnosticsAndReminderSettings(t *testing.T) {
	h := newHarness(t)
	s := h.withDiagnostics("list-one@example.com", 971, keyboardBody())
	other := h.signIn("list-two@example.com")
	h.mustRegister(installation(installID(972), "android", fcmToken(972)), other.access)
	android := diagnosticsBody()
	android["keyboard_setup_status"], android["keyboard_selected_status"] = "verified_enabled", "observed_denied"
	android["full_access_status"], android["onboarding_status"] = "unsupported", "completed"
	if res := h.reportDiagnostics(installID(972), android, other.access); res.status != http.StatusOK {
		t.Fatalf("report: %d", res.status)
	}
	if res := h.do(http.MethodGet, "/api/v1/admin/diagnostics", nil, nil); res.status != http.StatusUnauthorized {
		t.Fatalf("without a session: %d", res.status)
	}
	adminSession := h.signInAdmin()
	headers := adminSession.headers(h.cfg.Admin.CookieName)

	all := h.do(http.MethodGet, "/api/v1/admin/diagnostics", nil, headers)
	if all.status != http.StatusOK || all.num("total") != 2 || all.num("counts", "keyboard_not_verified") != 1 ||
		all.num("counts", "setup_incomplete") != 0 || all.num("counts", "all") != 2 {
		t.Fatalf("list: %d %s", all.status, all.raw)
	}
	if strings.Contains(string(all.raw), "list-one@example.com") {
		t.Fatal("the list shows a full e-mail")
	}
	one := h.do(http.MethodGet, "/api/v1/admin/diagnostics?filter=keyboard_not_verified&platform=ios", nil, headers)
	rows, _ := one.body["rows"].([]any)
	if one.num("total") != 1 || len(rows) != 1 || rows[0].(map[string]any)["user_id"] != s.userID {
		t.Fatalf("filtered: %s", one.raw)
	}
	for _, q := range []string{"?filter=everything", "?platform=web"} {
		if res := h.do(http.MethodGet, "/api/v1/admin/diagnostics"+q, nil, headers); res.status != http.StatusBadRequest {
			t.Errorf("%s: %d", q, res.status)
		}
	}
	card := h.do(http.MethodGet, "/api/v1/admin/users/"+s.userID+"/diagnostics", nil, headers)
	devices, _ := card.body["devices"].([]any)
	if card.status != http.StatusOK || len(devices) != 1 ||
		devices[0].(map[string]any)["diagnostics"].(map[string]any)["keyboard_setup_status"] != "not_verified" {
		t.Fatalf("user card: %d %s", card.status, card.raw)
	}

	settings := h.do(http.MethodGet, "/api/v1/admin/settings/reminders", nil, headers)
	if settings.status != http.StatusOK || settings.body["current"].(map[string]any)["enabled"] != false {
		t.Fatalf("settings: %s", settings.raw)
	}
	next := map[string]any{
		"enabled": true, "min_account_age_hours": 24, "quiet_start_hour": 22, "quiet_end_hour": 9,
		"types": map[string]any{
			"onboarding_incomplete":     map[string]any{"cooldown_hours": 72, "max_sends": 2},
			"keyboard_setup_incomplete": map[string]any{"cooldown_hours": 48, "max_sends": 11},
			"clipboard_help":            map[string]any{"cooldown_hours": 168, "max_sends": 1},
		},
	}
	if res := h.do(http.MethodPost, "/api/v1/admin/settings/reminders", next, headers); res.status != http.StatusBadRequest {
		t.Fatalf("max_sends 11 accepted: %d", res.status)
	}
	noCSRF := map[string]string{"Cookie": headers["Cookie"]}
	if res := h.do(http.MethodPost, "/api/v1/admin/settings/reminders", next, noCSRF); res.status != http.StatusForbidden {
		t.Fatalf("without CSRF: %d", res.status)
	}
	next["types"].(map[string]any)["keyboard_setup_incomplete"] = map[string]any{"cooldown_hours": 48, "max_sends": 3}
	if res := h.do(http.MethodPost, "/api/v1/admin/settings/reminders", next, headers); res.status != http.StatusOK ||
		res.body["current"].(map[string]any)["enabled"] != true {
		t.Fatalf("save: %d %s", res.status, res.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action = 'settings.reminders.update'`); n != 1 {
		t.Fatalf("audits = %d", n)
	}
	if !h.notify.RemindersEnabled(context.Background()) {
		t.Fatal("reminders are not on after saving")
	}
}
