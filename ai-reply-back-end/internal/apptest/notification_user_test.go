package apptest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/email"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
	"github.com/aireply/ai-reply-back-end/internal/push"
)

// preferred_language: PATCH /me жазады, GET /me қайтарады, белгісіз тіл — 400, кіру оны өзгертпейді.
func TestPreferredLanguageIsSavedAndValidated(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("language@example.com")

	me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access))
	if lang, ok := me.body["user"].(map[string]any)["preferred_language"]; !ok || lang != "" {
		t.Fatalf("a new account has not chosen: %s", me.raw)
	}
	cfg := h.do(http.MethodGet, "/api/v1/config", nil, nil)
	if features := cfg.body["features"].(map[string]any); features["preferred_language"] != true ||
		features["installations"] != true || features["push_notifications"] != true {
		t.Fatalf("features: %v", features)
	}

	if res := h.do(http.MethodPatch, "/api/v1/me", map[string]any{"preferred_language": "RU-kz"}, h.auth(s.access)); res.status != http.StatusOK {
		t.Fatalf("patch: %d %s", res.status, res.raw)
	}
	if got := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access)).str("user", "preferred_language"); got != "ru" {
		t.Fatalf("preferred_language = %q", got)
	}
	// The Android alias accepts it too, together with other profile fields.
	if res := h.do(http.MethodPost, "/api/v1/me", map[string]any{"preferred_language": "uz", "display_name": "Айгүл"},
		h.auth(s.access)); res.status != http.StatusOK || res.str("display_name") != "Айгүл" {
		t.Fatalf("post alias: %d %s", res.status, res.raw)
	}
	for _, bad := range []string{"de", "", "russian"} {
		res := h.do(http.MethodPatch, "/api/v1/me", map[string]any{"preferred_language": bad, "display_name": "Басқа"},
			h.auth(s.access))
		if res.status != http.StatusBadRequest || res.errorCode() != "INVALID_REQUEST" ||
			res.str("error", "details", "field") != "preferred_language" {
			t.Fatalf("%q: %d %s", bad, res.status, res.raw)
		}
	}
	me = h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access))
	if me.str("user", "preferred_language") != "uz" || me.str("profile", "display_name") != "Айгүл" {
		t.Fatalf("a rejected request changes nothing: %s", me.raw)
	}

	// Signing in again updates users.locale from the device, never the choice.
	h.clock.Advance(time.Minute) // past the OTP resend cooldown
	again := h.signIn("language@example.com")
	if got := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(again.access)).str("user", "preferred_language"); got != "uz" {
		t.Fatalf("after sign-in = %q", got)
	}
	admin := h.signInAdmin()
	detail := h.do(http.MethodGet, "/api/v1/admin/users/"+s.userID, nil, admin.headers(h.cfg.Admin.CookieName))
	if detail.str("user", "preferred_language") != "uz" {
		t.Fatalf("admin user detail: %s", detail.raw)
	}
}

// sentFor — осы хабарламаның жіберілген push-ы.
func (h *harness) sentFor(notificationID string) push.Message {
	h.t.Helper()
	for _, m := range h.push.messages() {
		if m.msg.Data["nid"] == notificationID {
			return m.msg
		}
	}
	h.t.Fatalf("no push was sent for %s", notificationID)
	return push.Message{}
}

// NotifyUser тілді серверде шешеді: preferred_language → соңғы құрылғының тілі → тіркелгі тілі → en.
func TestNotifyUserWritesInTheRecipientsLanguage(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	s := h.signIn("lang-chain@example.com") // account locale kk
	texts := map[string][2]string{"kk": {"Сәлем", "Мәтін"}, "ru": {"Привет", "Текст"}, "en": {"Hello", "Text"}, "uz": {"Salom", "Matn"}}
	notify := func(key string) notifications.NotifyResult {
		t.Helper()
		n := accountNotice(s.userID, key)
		n.Text = func(locale string) (string, string) { return texts[locale][0], texts[locale][1] }
		res, err := h.notify.NotifyUser(ctx, n)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := notify("lang:1"); res.Locale != "kk" {
		t.Fatalf("account locale: %+v", res)
	}
	device := installation(installID(401), "android", fcmToken(401))
	device["locale"] = "en"
	h.mustRegister(device, s.access)
	if res := notify("lang:2"); res.Locale != "en" {
		t.Fatalf("device locale: %+v", res)
	}
	h.setPreferredLanguage(s.access, "ru")
	res := notify("lang:3")
	if res.Locale != "ru" {
		t.Fatalf("preferred language: %+v", res)
	}
	h.tick()
	if msg := h.sentFor(res.NotificationID); msg.Title != "Привет" || msg.Body != "Текст" {
		t.Fatalf("push text = %+v", msg)
	}
	if got := h.text(`SELECT locale FROM notifications WHERE id = ?`, res.NotificationID); got != "ru" {
		t.Fatalf("stored locale = %q", got)
	}

	// Translation keys with {placeholders} filled from Params; params stay on the server.
	keyed, err := h.notify.NotifyUser(ctx, notifications.UserNotification{
		UserID: s.userID, IdempotencyKey: "lang:keys", Type: "account_notice", Category: domain.CategoryAccount,
		TitleKey: "email.otp.subject", BodyKey: "email.otp.expires", Params: map[string]string{"minutes": "7"},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.tick()
	if msg := h.sentFor(keyed.NotificationID); msg.Title != "Код подтверждения AI Reply" ||
		!strings.Contains(msg.Body, "7") || msg.Data["minutes"] != "" {
		t.Fatalf("keyed push = %+v", msg)
	}
	if got := h.text(`SELECT params FROM notifications WHERE id = ?`, keyed.NotificationID); got != `{"minutes":"7"}` {
		t.Fatalf("params = %s", got)
	}
	for name, n := range map[string]notifications.UserNotification{
		"missing key": {UserID: s.userID, IdempotencyKey: "lang:bad1", Type: "account_notice",
			TitleKey: "push.nothing.title", BodyKey: "push.nothing.body"},
		"no text":  {UserID: s.userID, IdempotencyKey: "lang:bad2", Type: "account_notice"},
		"bad type": {UserID: s.userID, IdempotencyKey: "lang:bad3", Type: "Bad Type", Text: accountNotice("", "").Text},
		"bad key":  {UserID: s.userID, IdempotencyKey: "has space", Type: "account_notice", Text: accountNotice("", "").Text},
	} {
		if _, err := h.notify.NotifyUser(ctx, n); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Хабарлама хаты бір рет, расталған мекенжайға, алушының тілінде кетеді.
func TestNotificationEmailIsSentOnceToTheVerifiedAddress(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	s := h.signIn("mail-me@example.com")
	h.setPreferredLanguage(s.access, "ru")
	h.mustRegister(installation(installID(402), "ios", fcmToken(402)), s.access)
	n := accountNotice(s.userID, "subscription_activated:payment:pay-9")
	n.Type, n.Category, n.Email = domain.TypeSubscriptionActivated, domain.CategorySubscription, true
	n.Params = map[string]string{"plan": "Pro"}
	first, err := h.notify.NotifyUser(ctx, n)
	if err != nil || !first.Email || first.Devices != 1 {
		t.Fatalf("first = %+v %v", first, err)
	}
	again, err := h.notify.NotifyUser(ctx, n)
	if err != nil || again.Created || again.Email {
		t.Fatalf("again = %+v %v", again, err)
	}
	h.tick()
	h.tick()
	mails := h.mail.messages()
	if len(mails) != 1 {
		t.Fatalf("mails = %d", len(mails))
	}
	did := h.text(`SELECT id FROM notification_deliveries WHERE notification_id = ? AND channel = 'email'`, first.NotificationID)
	m := mails[0]
	if m.To != "mail-me@example.com" || m.Content.Subject != "subscription_activated ru" || m.Content.Text != "Pro" ||
		m.IdempotencyKey != "notification-"+did || m.Reference != did {
		t.Fatalf("mail = %+v", m)
	}
	if got := h.text(`SELECT status FROM notification_deliveries WHERE id = ?`, did); got != domain.DeliveryAccepted {
		t.Fatalf("email delivery = %s", got)
	}
	pushed := h.push.messages()
	if len(pushed) != 1 || pushed[0].msg.Data["plan"] != "" {
		t.Fatalf("the push goes too, without the e-mail params: %+v", pushed)
	}
	if h.dbContains("mail-me@example.com\"") {
		t.Fatal("the address is read at send time, never copied into the outbox")
	}
}

// Пошта бапталмаса хат «skipped» болып жазылады; push бөлек жүреді.
func TestNotificationEmailIsSkippedWithoutAMailer(t *testing.T) {
	h := newHarness(t)
	h.notify.WithEmail(nil, nil)
	s := h.signIn("no-mailer@example.com")
	n := accountNotice(s.userID, "no-mailer:1")
	n.Email = true
	res, err := h.notify.NotifyUser(context.Background(), n)
	if err != nil || res.Email || res.Skipped != notifications.SkipNoDevices {
		t.Fatalf("res = %+v %v", res, err)
	}
	if got := h.text(`SELECT status || ':' || error_code FROM notification_deliveries WHERE notification_id = ? AND channel = 'email'`,
		res.NotificationID); got != "skipped:email_disabled" {
		t.Fatalf("email row = %q", got)
	}
	h.tick()
	if len(h.mail.messages()) != 0 {
		t.Fatal("nothing is mailed without a mailer")
	}
	admin := h.signInAdmin()
	meta := h.do(http.MethodGet, "/api/v1/admin/notifications", nil, admin.headers(h.cfg.Admin.CookieName))
	if meta.body["status"].(map[string]any)["email"] != false {
		t.Fatalf("status: %s", meta.raw)
	}
}

// Хат: уақытша қате қайталанады, бас тарту — сәтсіз, үлгісі жоқ түр, расталмаған пошта, өшірілген санат — skipped.
func TestNotificationEmailOutcomes(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	s := h.signIn("mail-outcomes@example.com")
	notify := func(key, kind string) string {
		t.Helper()
		n := accountNotice(s.userID, key)
		n.Type, n.Email = kind, true
		res, err := h.notify.NotifyUser(ctx, n)
		if err != nil {
			t.Fatal(err)
		}
		return res.NotificationID
	}
	emailRow := func(id string) string {
		t.Helper()
		return h.text(`SELECT status || ':' || error_code FROM notification_deliveries WHERE notification_id = ? AND channel = 'email'`, id)
	}

	h.mail.failNext(&email.DeliveryError{Status: 503})
	retried := notify("mail:retry", "account_notice")
	h.tick()
	if got := emailRow(retried); got != "retrying:HTTP_503" {
		t.Fatalf("after a 503: %s", got)
	}
	h.pushClock.Advance(time.Minute)
	h.tick()
	if got := emailRow(retried); got != "provider_accepted:" {
		t.Fatalf("after the retry: %s", got)
	}

	h.mail.failNext(&email.DeliveryError{Status: 422, Name: "validation_error"})
	rejected := notify("mail:rejected", "account_notice")
	h.tick()
	if got := emailRow(rejected); got != "provider_failed:HTTP_422" {
		t.Fatalf("a rejection is final: %s", got)
	}

	if got := emailRow(notify("mail:template", "no_template")); got != "queued:" {
		t.Fatalf("queued first: %s", got)
	}
	h.tick()
	if got := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE error_code = 'email_template_missing'`); got != 1 {
		t.Fatalf("a type without an e-mail template is skipped: %d", got)
	}

	if _, err := h.db.Writer().Exec(`UPDATE users SET email_verified_at = NULL WHERE id = ?`, s.userID); err != nil {
		t.Fatal(err)
	}
	unverified := notify("mail:unverified", "account_notice")
	h.tick()
	if got := emailRow(unverified); got != "skipped:no_verified_email" {
		t.Fatalf("an unverified address gets nothing: %s", got)
	}

	if res := h.do(http.MethodPut, "/api/v1/me/notification-preferences",
		map[string]any{"preferences": map[string]bool{"account": false}}, h.auth(s.access)); res.status != http.StatusOK {
		t.Fatalf("preferences: %d", res.status)
	}
	if got := emailRow(notify("mail:silenced", "account_notice")); got != "skipped:disabled_by_user" {
		t.Fatalf("a switched-off category: %s", got)
	}
	if n := len(h.mail.messages()); n != 1 {
		t.Fatalf("mails = %d, want only the retried one", n)
	}

	phone := h.signIn("+7 705 444 55 66")
	n := accountNotice(phone.userID, "mail:phone")
	n.Email = true
	res, err := h.notify.NotifyUser(ctx, n)
	if err != nil {
		t.Fatal(err)
	}
	h.tick()
	if got := emailRow(res.NotificationID); got != "skipped:no_verified_email" {
		t.Fatalf("a phone account has no address: %s", got)
	}
}

// Сақтау мерзімі: аяқталған ескі жолдар өшеді, кезектегілер ешқашан; науқан есебі қалады.
func TestRetentionKeepsPendingDeliveriesAndCampaignTotals(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	old := h.pushClock.Now().AddDate(0, 0, -h.cfg.Push.RetentionDays-1).UnixMilli()

	finished := h.signIn("retention-finished@example.com")
	oldDone := h.notifyUser(finished.userID, "retention:done") // no device: one skipped row
	pending := h.signIn("retention-pending@example.com")
	h.mustRegister(installation(installID(501), "android", fcmToken(501)), pending.access)
	oldQueued := h.notifyUser(pending.userID, "retention:queued") // not sent yet
	fresh := h.notifyUser(finished.userID, "retention:fresh")

	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	campaign := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{
		"send": true, "audience": map[string]any{"user_ids": []string{pending.userID}},
	}), withKey(headers, "retention-campaign-01"))
	h.notify.Tick(ctx) // the campaign goes out together with the queued notification…
	if n := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE status = 'provider_accepted'`); n != 2 {
		t.Fatalf("accepted = %d", n)
	}
	// …so put the user notification back in the queue, as if its send were still due.
	if _, err := h.db.Writer().Exec(`UPDATE notification_deliveries SET status = 'retrying', next_attempt_at = ?
		WHERE notification_id = ?`, h.pushClock.Now().Add(time.Hour).UnixMilli(), oldQueued.NotificationID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{oldDone.NotificationID, oldQueued.NotificationID} {
		if _, err := h.db.Writer().Exec(`UPDATE notifications SET created_at = ? WHERE id = ?`, old, id); err != nil {
			t.Fatal(err)
		}
		if _, err := h.db.Writer().Exec(`UPDATE notification_deliveries SET created_at = ? WHERE notification_id = ?`, old, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.db.Writer().Exec(`UPDATE notification_deliveries SET created_at = ? WHERE campaign_id = ?`, old, campaign.str("id")); err != nil {
		t.Fatal(err)
	}

	deliveries, removed := h.notify.Retain(ctx)
	if deliveries != 2 || removed != 1 {
		t.Fatalf("removed deliveries %d, notifications %d", deliveries, removed)
	}
	for id, want := range map[string]int{oldDone.NotificationID: 0, oldQueued.NotificationID: 1, fresh.NotificationID: 1} {
		if n := h.scalar(`SELECT COUNT(*) FROM notifications WHERE id = ?`, id); n != want {
			t.Errorf("notification %s: %d, want %d", id[:8], n, want)
		}
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE notification_id = ?`, oldQueued.NotificationID); n != 1 {
		t.Fatal("a pending delivery must never be removed")
	}
	detail := h.do(http.MethodGet, "/api/v1/admin/notifications/campaigns/"+campaign.str("id"), nil, headers)
	if detail.num("stats", "provider_accepted") != 1 || detail.num("stats", "by_language", "kk", "provider_accepted") != 1 {
		t.Fatalf("the campaign keeps its final totals: %s", detail.raw)
	}
}

// Хабарлама жолы — оқиғаның «кезеңге бір рет» кепілі: сақтау мерзімі қысқа болса да ай бойы қалады.
func TestShortRetentionKeepsTheOncePerPeriodGuard(t *testing.T) {
	h := newHarness(t, withEnv("RETENTION_NOTIFICATIONS_DAYS", "7"))
	ctx := context.Background()
	s := h.signIn("short-retention@example.com")
	h.notifyUser(s.userID, "quota_low:month:2026-03") // no device: one skipped row

	h.pushClock.Advance(8 * 24 * time.Hour)
	if deliveries, removed := h.notify.Retain(ctx); deliveries != 1 || removed != 0 {
		t.Fatalf("day 8: removed deliveries %d, notifications %d", deliveries, removed)
	}
	if again := h.notifyUser(s.userID, "quota_low:month:2026-03"); again.Created {
		t.Fatal("the same month's notice was created again")
	}
	h.pushClock.Advance(40 * 24 * time.Hour)
	if _, removed := h.notify.Retain(ctx); removed != 1 {
		t.Fatalf("the guard outlives its month, not more: removed %d", removed)
	}
}

// Жеткізулер тізімі арна, көз, түр және тіл бойынша сүзіледі.
func TestDeliveriesListFilters(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("delivery-list@example.com")
	h.mustRegister(installation(installID(601), "android", fcmToken(601)), s.access)
	n := accountNotice(s.userID, "list:1")
	n.Email = true
	if _, err := h.notify.NotifyUser(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{"send": true}),
		withKey(headers, "list-campaign-000001"))
	h.tick()

	for query, want := range map[string]float64{
		"":                          3,
		"?channel=email":            1,
		"?channel=push":             2,
		"?source=automatic":         2,
		"?source=campaign":          1,
		"?type=account_notice":      2,
		"?type=campaign":            1,
		"?locale=kk":                3,
		"?locale=ru":                0,
		"?user_id=" + s.userID:      3,
		"?status=provider_accepted": 3,
	} {
		res := h.do(http.MethodGet, "/api/v1/admin/notifications/deliveries"+query, nil, headers)
		if res.status != http.StatusOK || res.num("total") != want {
			t.Errorf("%q: %d %s", query, res.status, res.raw)
		}
	}
	list := h.do(http.MethodGet, "/api/v1/admin/notifications/deliveries?channel=email", nil, headers)
	row := list.body["deliveries"].([]any)[0].(map[string]any)
	if row["channel"] != "email" || row["provider"] != "resend" || row["push"] != "" || row["locale"] != "kk" ||
		row["installation_id"] != "" || strings.Contains(string(list.raw), "delivery-list@example.com") {
		t.Fatalf("email row: %v", row)
	}
	pushRow := h.do(http.MethodGet, "/api/v1/admin/notifications/deliveries?channel=push&source=automatic", nil, headers)
	if !strings.Contains(string(pushRow.raw), `"push":"fcm:`) || !strings.Contains(string(pushRow.raw), "Samsung SM-S928B") {
		t.Fatalf("push row: %s", pushRow.raw)
	}
	for _, query := range []string{"?channel=sms", "?source=other", "?status=lost", "?locale=de", "?platform=web"} {
		if res := h.do(http.MethodGet, "/api/v1/admin/notifications/deliveries"+query, nil, headers); res.status != http.StatusBadRequest {
			t.Errorf("%s: %d", query, res.status)
		}
	}
}
