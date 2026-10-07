package apptest

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/installations"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
	"github.com/aireply/ai-reply-back-end/internal/push"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// ---------------------------------------------------------------- helpers

// fcmToken — FCM registration token пішіміндегі тест токені (iOS-та да осындай).
func fcmToken(seed int) string {
	return fmt.Sprintf("fcm-token-%04d:APA91bHPRgkF3JUikC4ENAHEeMrd41Zxv3hVZjC9KtT8OvPVGJ", seed)
}

func installID(seed int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", seed) }

// installation — POST /api/v1/installations денесі.
func installation(id, platform, token string) map[string]any {
	body := map[string]any{
		"installation_id": id, "platform": platform, "app_version": "1.3.2", "app_build": "142",
		"os_version": "26.5", "device_model": "iPhone17,1", "manufacturer": "Apple", "locale": "kk",
		"timezone": "Asia/Almaty", "notification_permission": "authorized", "notifications_enabled": true,
	}
	if platform == domain.PlatformAndroid {
		body["device_model"], body["manufacturer"], body["os_version"] = "SM-S928B", "samsung", "15"
	}
	if token != "" {
		body["push"] = map[string]any{"provider": "fcm", "token": token}
	}
	return body
}

func (h *harness) register(body map[string]any, access string) response {
	h.t.Helper()
	headers := map[string]string{}
	if access != "" {
		headers = h.auth(access)
	}
	return h.do(http.MethodPost, "/api/v1/installations", body, headers)
}

func (h *harness) mustRegister(body map[string]any, access string) response {
	h.t.Helper()
	res := h.register(body, access)
	if res.status != http.StatusOK {
		h.t.Fatalf("register installation: %d %s", res.status, res.raw)
	}
	return res
}

func (h *harness) tick() { h.notify.Tick(context.Background()) }

func (h *harness) scalar(query string, args ...any) int {
	h.t.Helper()
	var n int
	if err := h.db.Reader().QueryRow(query, args...).Scan(&n); err != nil {
		h.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (h *harness) text(query string, args ...any) string {
	h.t.Helper()
	var v sql.NullString
	if err := h.db.Reader().QueryRow(query, args...).Scan(&v); err != nil {
		h.t.Fatalf("%s: %v", query, err)
	}
	return v.String
}

// accountNotice — бір тілдегі қарапайым хабарлама (мәтіні тілге қарамайды).
func accountNotice(userID, key string) notifications.UserNotification {
	return notifications.UserNotification{
		UserID: userID, IdempotencyKey: key, Type: "account_notice", Category: domain.CategoryAccount,
		Link: "aireply://settings",
		Text: func(string) (string, string) { return "Тіркелгі", "Хабарлама мәтіні" },
	}
}

func (h *harness) notifyUser(userID, key string) notifications.NotifyResult {
	h.t.Helper()
	res, err := h.notify.NotifyUser(context.Background(), accountNotice(userID, key))
	if err != nil {
		h.t.Fatalf("NotifyUser: %v", err)
	}
	return res
}

func sentTokens(p *fakePush) map[string]int {
	out := map[string]int{}
	for _, m := range p.messages() {
		out[m.token]++
	}
	return out
}

func withKey(headers map[string]string, key string) map[string]string {
	out := map[string]string{"Idempotency-Key": key}
	for k, v := range headers {
		out[k] = v
	}
	return out
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// ---------------------------------------------------------------- installations

// Кірмеген қосымша да тіркеледі; қайта тіркеу жаңа жол ашпайды.
func TestAnonymousInstallationIsRegisteredOnce(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 3; i++ {
		res := h.mustRegister(installation(installID(1), "android", fcmToken(1)), "")
		if res.body["attached"] != false || res.str("push_status") != domain.PushActive || res.body["push_available"] != true {
			t.Fatalf("anonymous registration: %s", res.raw)
		}
		if res.body["preferences"] != nil {
			t.Fatal("preferences belong to an account")
		}
		if strings.Contains(string(res.raw), fcmToken(1)) || strings.Contains(string(res.raw), "hash") {
			t.Fatal("the response must never echo the token or its hash")
		}
	}
	if n := h.scalar(`SELECT COUNT(*) FROM app_installations`); n != 1 {
		t.Fatalf("installations = %d, want 1", n)
	}
	if h.dbContains(fcmToken(1)) {
		t.Fatal("the raw push token is stored unencrypted")
	}
}

// Анонимді орнату кіргеннен кейін сол жол болып қалады (қайталанбайды).
func TestAnonymousInstallationBecomesTheAccountsDevice(t *testing.T) {
	h := newHarness(t)
	h.mustRegister(installation(installID(2), "ios", fcmToken(2)), "")
	s := h.signIn("anon-to-auth@example.com")
	res := h.mustRegister(installation(installID(2), "ios", fcmToken(2)), s.access)
	if res.body["attached"] != true {
		t.Fatalf("authenticated registration must attach: %s", res.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM app_installations`); n != 1 {
		t.Fatalf("installations = %d, want the same row", n)
	}
	if got := h.text(`SELECT user_id FROM app_installations WHERE installation_id = ?`, installID(2)); got != s.userID {
		t.Fatalf("user_id = %q, want %q", got, s.userID)
	}
	prefs, _ := res.body["preferences"].(map[string]any)
	if prefs["security"] != true || prefs["marketing"] != true {
		t.Fatalf("an attached installation gets the account's preferences: %s", res.raw)
	}
}

// Қосымша body-дегі user_id-ге ешқашан сенбейміз: ондай өріс қабылданбайды.
func TestInstallationNeverTakesAUserFromTheBody(t *testing.T) {
	h := newHarness(t)
	victim := h.signIn("victim@example.com")
	body := installation(installID(3), "ios", fcmToken(3))
	body["user_id"] = victim.userID
	if res := h.register(body, ""); res.status != http.StatusBadRequest {
		t.Fatalf("user_id in the body must be refused: %d", res.status)
	}
	apns := installation(installID(3), "ios", "")
	apns["push"] = map[string]any{"provider": "apns", "token": strings.Repeat("ab", 32)}
	if res := h.register(apns, ""); res.status != http.StatusBadRequest || res.str("error", "details", "field") != "push.provider" {
		t.Fatalf("both platforms register FCM tokens: %d %s", res.status, res.raw)
	}
}

// Мерзімі өткен токен анонимді тіркеуге айналмайды (әйтпесе құрылғы ажырап қалар еді).
func TestInvalidAccessTokenDoesNotDetach(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("expired-token@example.com")
	h.mustRegister(installation(installID(4), "android", fcmToken(4)), s.access)
	if res := h.register(installation(installID(4), "android", fcmToken(4)), "not-a-valid-token"); res.status != http.StatusUnauthorized {
		t.Fatalf("invalid token: %d", res.status)
	}
	if got := h.text(`SELECT user_id FROM app_installations WHERE installation_id = ?`, installID(4)); got != s.userID {
		t.Fatal("a refused token must leave the installation attached")
	}
}

// Шығу: орнату тіркелгіден ажырайды; анонимді тіркеу де ажыратады.
func TestLogoutDetachesTheInstallation(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("logout@example.com")
	h.mustRegister(installation(installID(5), "ios", fcmToken(5)), s.access)

	res := h.do(http.MethodPost, "/api/v1/auth/logout", map[string]any{"refresh_token": s.refresh},
		map[string]string{"X-Installation-ID": installID(5)})
	if res.status != http.StatusOK {
		t.Fatalf("logout: %d %s", res.status, res.raw)
	}
	if got := h.text(`SELECT user_id FROM app_installations WHERE installation_id = ?`, installID(5)); got != "" {
		t.Fatalf("still attached to %q after logout", got)
	}
	if res := h.notifyUser(s.userID, "after-logout:1"); res.Devices != 0 || res.Skipped != notifications.SkipNoDevices {
		t.Fatalf("a signed-out phone receives nothing: %+v", res)
	}

	// The explicit detach call and an anonymous registration do the same.
	other := h.signIn("logout2@example.com")
	h.mustRegister(installation(installID(6), "ios", fcmToken(6)), other.access)
	if res := h.do(http.MethodPost, "/api/v1/installations/"+installID(6)+"/detach", map[string]any{}, h.auth(other.access)); res.status != http.StatusOK || res.body["detached"] != true {
		t.Fatalf("detach: %d %s", res.status, res.raw)
	}
	h.mustRegister(installation(installID(6), "ios", fcmToken(6)), other.access)
	h.mustRegister(installation(installID(6), "ios", fcmToken(6)), "")
	if got := h.text(`SELECT user_id FROM app_installations WHERE installation_id = ?`, installID(6)); got != "" {
		t.Fatal("an anonymous registration means the app is signed out")
	}
	if res := h.do(http.MethodPost, "/api/v1/installations/bad id/detach", map[string]any{}, nil); res.status == http.StatusOK {
		t.Fatal("a malformed installation id is refused")
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

// Шектеу орнату бойынша есептеледі (идентификатор денеде, қосымшалар тақырып жібермейді):
// бір IP-дегі басқа телефон шектелмейді.
func TestInstallationBudgetIsPerInstallation(t *testing.T) {
	h := newHarness(t, withEnv("RATE_GENERIC_PER_MINUTE", "2"))
	register := func(seed int) response {
		return h.register(installation(installID(seed), "android", fcmToken(seed)), "")
	}
	opened := func(seed int) int {
		return h.do(http.MethodPost, "/api/v1/notifications/opened",
			map[string]any{"installation_id": installID(seed), "delivery_id": "aaaaaaaa-bbbb"}, nil).status
	}
	for i := 0; i < 2; i++ {
		if res := register(83); res.status != http.StatusOK {
			t.Fatalf("registration %d: %d", i, res.status)
		}
	}
	third := register(83)
	if third.status != http.StatusTooManyRequests || third.header.Get("Retry-After") == "" ||
		third.num("error", "details", "retry_after_seconds") < 1 {
		t.Fatalf("third registration of the same installation: %d %s", third.status, third.raw)
	}
	if status := opened(83); status != http.StatusTooManyRequests {
		t.Fatalf("\"opened\" shares the installation's budget: %d", status)
	}
	if res := register(84); res.status != http.StatusOK {
		t.Fatalf("another phone behind the same address was throttled: %d", res.status)
	}
	if status := opened(84); status != http.StatusOK {
		t.Fatalf("another phone's \"opened\": %d", status)
	}
}

// Токен жаңарды: ескісі ұмытылады.
func TestTokenRefreshReplacesTheToken(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("refresh-token@example.com")
	h.mustRegister(installation(installID(7), "android", fcmToken(71)), s.access)
	h.mustRegister(installation(installID(7), "android", fcmToken(72)), s.access)
	if got := h.text(`SELECT push_token_hash FROM app_installations WHERE installation_id = ?`, installID(7)); got != push.TokenHash(fcmToken(72)) {
		t.Fatal("the new token must replace the old one")
	}
	h.notifyUser(s.userID, "refresh:1")
	h.tick()
	sent := sentTokens(h.push)
	if sent[fcmToken(72)] != 1 || sent[fcmToken(71)] != 0 {
		t.Fatalf("sent = %v", sent)
	}
}

// Бір токен бір орнатуда ғана: қайта орнатылған қосымша ескі жолдан токенді алады.
func TestADuplicateTokenMovesToTheNewInstallation(t *testing.T) {
	h := newHarness(t)
	old := h.signIn("old-install@example.com")
	h.mustRegister(installation(installID(8), "ios", fcmToken(8)), old.access)
	h.mustRegister(installation(installID(9), "ios", fcmToken(8)), "")

	if n := h.scalar(`SELECT COUNT(*) FROM app_installations WHERE push_token_hash = ?`, push.TokenHash(fcmToken(8))); n != 1 {
		t.Fatalf("token held by %d installations", n)
	}
	if got := h.text(`SELECT push_status FROM app_installations WHERE installation_id = ?`, installID(8)); got != domain.PushReplaced {
		t.Fatalf("old installation status = %q", got)
	}
	// The old account's notification finds no reachable device any more.
	if res := h.notifyUser(old.userID, "moved:1"); res.Devices != 0 {
		t.Fatalf("queued %d deliveries to a phone that now belongs to someone else", res.Devices)
	}
}

// Бір телефонда басқа тіркелгі: алдыңғы тіркелгінің кезектегі хабарламасы көрсетілмейді.
func TestAnotherAccountOnTheSamePhoneNeverSeesThePreviousAccountsPush(t *testing.T) {
	h := newHarness(t)
	a := h.signIn("person-a@example.com")
	b := h.signIn("person-b@example.com")
	h.mustRegister(installation(installID(10), "ios", fcmToken(10)), a.access)
	queued := h.notifyUser(a.userID, "private:1")
	if queued.Devices != 1 {
		t.Fatalf("queued = %+v", queued)
	}
	// Before the worker runs, A signs out and B signs in on the same phone.
	h.mustRegister(installation(installID(10), "ios", fcmToken(10)), b.access)
	h.tick()
	if len(h.push.messages()) != 0 {
		t.Fatal("A's notification reached the phone B is signed in on")
	}
	if got := h.text(`SELECT error_code FROM notification_deliveries WHERE notification_id = ?`, queued.NotificationID); got != "recipient_changed" {
		t.Fatalf("delivery error = %q", got)
	}
	h.notifyUser(b.userID, "private:2")
	h.tick()
	if len(h.push.messages()) != 1 {
		t.Fatal("B's own notification must be delivered")
	}
}

// ---------------------------------------------------------------- delivery

// Бір оқиға — бір хабарлама; қолданушының әр құрылғысына (Android және iOS, екеуі де FCM) бір-бірден.
func TestNotifyUserIsIdempotentAndReachesEveryDevice(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("two-phones@example.com")
	h.mustRegister(installation(installID(11), "android", fcmToken(11)), s.access)
	h.mustRegister(installation(installID(12), "ios", fcmToken(12)), s.access)

	first := h.notifyUser(s.userID, "subscription_activated:payment:pay-1")
	again := h.notifyUser(s.userID, "subscription_activated:payment:pay-1")
	if !first.Created || first.Devices != 2 || again.Created || again.NotificationID != first.NotificationID || again.Devices != 0 {
		t.Fatalf("first = %+v, again = %+v", first, again)
	}
	h.tick()
	h.tick()
	sent := h.push.messages()
	if len(sent) != 2 || sentTokens(h.push)[fcmToken(11)] != 1 || sentTokens(h.push)[fcmToken(12)] != 1 {
		t.Fatalf("sent %d messages, want exactly one per device", len(sent))
	}
	msg := sent[0].msg
	if msg.Data["nid"] != first.NotificationID || msg.Data["did"] == "" || msg.Data["link"] != "aireply://settings" ||
		msg.Data["category"] != domain.CategoryAccount || msg.Data["type"] != "account_notice" || !msg.Important ||
		msg.CollapseID != first.NotificationID || msg.TTL != notifications.DefaultTTL {
		t.Fatalf("payload = %+v", msg)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE status = 'provider_accepted' AND sent_at IS NOT NULL
		AND provider = 'fcm' AND channel = 'push'`); n != 2 {
		t.Fatalf("accepted deliveries = %d", n)
	}
	if n := h.scalar(`SELECT COUNT(DISTINCT platform) FROM notification_deliveries WHERE notification_id = ?`, first.NotificationID); n != 2 {
		t.Fatalf("platforms = %d, want android and ios", n)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notifications`); n != 1 {
		t.Fatalf("notifications = %d", n)
	}
}

// Уақытша қате: артқа шегініспен қайталанады, сосын жеткізіледі.
func TestTransientFailureIsRetriedWithBackoff(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("retry@example.com")
	h.mustRegister(installation(installID(13), "android", fcmToken(13)), s.access)
	h.push.respond(push.Result{Outcome: push.Retry, Code: "UNAVAILABLE", StatusCode: 503})
	res := h.notifyUser(s.userID, "retry:1")

	h.tick()
	var status, code string
	var next int64
	if err := h.db.Reader().QueryRow(`SELECT status, error_code, next_attempt_at FROM notification_deliveries WHERE notification_id = ?`,
		res.NotificationID).Scan(&status, &code, &next); err != nil {
		t.Fatal(err)
	}
	wait := time.UnixMilli(next).Sub(h.pushClock.Now())
	if status != domain.DeliveryRetrying || code != "UNAVAILABLE" || wait < 4*time.Second || wait > 6*time.Second {
		t.Fatalf("after a 503: %s %s, next attempt in %v", status, code, wait)
	}
	h.tick() // not due yet
	if len(h.push.messages()) != 1 {
		t.Fatal("retried before the backoff elapsed")
	}
	h.pushClock.Advance(10 * time.Second)
	h.tick()
	if got := h.text(`SELECT status FROM notification_deliveries WHERE notification_id = ?`, res.NotificationID); got != domain.DeliveryAccepted {
		t.Fatalf("status after retry = %s", got)
	}
	if len(h.push.messages()) != 2 {
		t.Fatalf("sends = %d, want 2", len(h.push.messages()))
	}
}

// Қайталау шектеулі: 5 әрекеттен кейін тоқтайды.
func TestRetriesStopAfterMaxAttempts(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("always-failing@example.com")
	h.mustRegister(installation(installID(14), "android", fcmToken(14)), s.access)
	for i := 0; i < 10; i++ {
		h.push.respond(push.Result{Outcome: push.Retry, Code: "INTERNAL", StatusCode: 500})
	}
	res := h.notifyUser(s.userID, "retry:max")
	for i := 0; i < 8; i++ {
		h.tick()
		h.pushClock.Advance(time.Hour)
	}
	if got := h.text(`SELECT status FROM notification_deliveries WHERE notification_id = ?`, res.NotificationID); got != domain.DeliveryFailed {
		t.Fatalf("status = %s", got)
	}
	if n := len(h.push.messages()); n != h.cfg.Push.MaxAttempts {
		t.Fatalf("attempts = %d, want %d", n, h.cfg.Push.MaxAttempts)
	}
}

// FCM «токен өлі» десе (iOS та, Android та): орнату токені өшеді, қайталау жоқ.
func TestInvalidTokenIsDeactivated(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("dead-token@example.com")
	h.mustRegister(installation(installID(15), "ios", fcmToken(15)), s.access)
	h.push.respond(push.Result{Outcome: push.InvalidToken, Code: "UNREGISTERED", StatusCode: 404})
	res := h.notifyUser(s.userID, "invalid:1")
	h.tick()
	h.pushClock.Advance(time.Hour)
	h.tick()
	if got := h.text(`SELECT status FROM notification_deliveries WHERE notification_id = ?`, res.NotificationID); got != domain.DeliveryInvalidToken {
		t.Fatalf("delivery = %s", got)
	}
	if len(h.push.messages()) != 1 {
		t.Fatal("a dead token must not be retried")
	}
	var status, reason string
	var sealed sql.NullString
	if err := h.db.Reader().QueryRow(`SELECT push_status, push_status_reason, push_token_sealed FROM app_installations WHERE installation_id = ?`,
		installID(15)).Scan(&status, &reason, &sealed); err != nil {
		t.Fatal(err)
	}
	if status != domain.PushInvalid || reason != "UNREGISTERED" || sealed.Valid {
		t.Fatalf("installation: %s %s %v", status, reason, sealed)
	}
	if again := h.notifyUser(s.userID, "invalid:2"); again.Devices != 0 {
		t.Fatal("nothing may be queued for a deactivated token")
	}
	// The app registers the same dead token again (a daily refresh, a new
	// language): it stays invalid, and the answer tells the app to replace it.
	if res := h.mustRegister(installation(installID(15), "ios", fcmToken(15)), s.access); res.str("push_status") != domain.PushInvalid {
		t.Fatalf("the dead token came back: %s", res.raw)
	}
	if again := h.notifyUser(s.userID, "invalid:3"); again.Devices != 0 {
		t.Fatal("a dead token registered again must not be queued")
	}
	// The app registers a fresh token: push works again.
	if res := h.mustRegister(installation(installID(15), "ios", fcmToken(151)), s.access); res.str("push_status") != domain.PushActive {
		t.Fatalf("a new token must reactivate the installation: %s", res.raw)
	}
	if again := h.notifyUser(s.userID, "invalid:4"); again.Devices != 1 {
		t.Fatal("a new token must reactivate the installation")
	}
	// The delivery log keeps the token each send used, not the device's current one.
	admin := h.signInAdmin()
	log := h.do(http.MethodGet, "/api/v1/admin/notifications/deliveries?status=invalid_token", nil,
		admin.headers(h.cfg.Admin.CookieName))
	rows, _ := log.body["deliveries"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["push"] != "fcm:"+push.TokenHash(fcmToken(15))[:8] {
		t.Fatalf("delivery log: %s", log.raw)
	}
}

// SENDER_ID_MISMATCH сервердің Firebase жобасынан да болуы мүмкін: сол токен қайта тіркелсе, белсенді болады.
func TestSenderIDMismatchTokenCanComeBack(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("sender-mismatch@example.com")
	h.mustRegister(installation(installID(85), "android", fcmToken(85)), s.access)
	h.push.respond(push.Result{Outcome: push.InvalidToken, Code: "SENDER_ID_MISMATCH", StatusCode: 403})
	h.notifyUser(s.userID, "mismatch:1")
	h.tick()
	if got := h.text(`SELECT push_status FROM app_installations WHERE installation_id = ?`, installID(85)); got != domain.PushInvalid {
		t.Fatalf("after SENDER_ID_MISMATCH: %s", got)
	}
	// The server's Firebase settings were fixed; the app sends the same token.
	if res := h.mustRegister(installation(installID(85), "android", fcmToken(85)), s.access); res.str("push_status") != domain.PushActive {
		t.Fatalf("re-registered: %s", res.raw)
	}
	if again := h.notifyUser(s.userID, "mismatch:2"); again.Devices != 1 {
		t.Fatalf("again = %+v", again)
	}
}

// Сервер тоқтағанда: басталған жіберудің нәтижесі жазылады, басталмағаны кезекке қайтады.
func TestStoppingTheWorkerSavesSendsInFlightAndReleasesTheRest(t *testing.T) {
	h := newHarness(t, withEnv("PUSH_WORKER_CONCURRENCY", "1"))
	s := h.signIn("shutdown@example.com")
	h.mustRegister(installation(installID(86), "android", fcmToken(86)), s.access)
	h.mustRegister(installation(installID(87), "ios", fcmToken(87)), s.access)
	res := h.notifyUser(s.userID, "shutdown:1")
	if res.Devices != 2 {
		t.Fatalf("queued = %+v", res)
	}
	ctx, stop := context.WithCancel(context.Background())
	h.push.whenSending(stop) // the stop signal arrives while the first push is with FCM
	h.notify.Tick(ctx)
	h.push.whenSending(nil)

	rows := func() string {
		t.Helper()
		return h.text(`SELECT group_concat(status || ':' || attempt_count, ' ') FROM
			(SELECT status, attempt_count FROM notification_deliveries WHERE notification_id = ? ORDER BY status)`, res.NotificationID)
	}
	if got := rows(); got != "provider_accepted:1 queued:0" || len(h.push.messages()) != 1 {
		t.Fatalf("after the stop: %s, sends %d", got, len(h.push.messages()))
	}
	h.tick()
	if got := rows(); got != "provider_accepted:1 provider_accepted:1" || len(h.push.messages()) != 2 {
		t.Fatalf("after the restart: %s, sends %d", got, len(h.push.messages()))
	}
}

// Дерекқордан оқу сәтсіз болса, жеткізу кейін қайталанады, жоғалмайды.
func TestAFailedReadIsRetriedNotSkipped(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("read-failure@example.com")
	h.mustRegister(installation(installID(88), "android", fcmToken(88)), s.access)
	res := h.notifyUser(s.userID, "read-failure:1")

	// Another worker whose read connections fail.
	otherDB, err := database.Open(database.Options{Path: h.dbPath, MaxReadConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer otherDB.Close()
	broken := notifications.New(notifications.Deps{
		Repo: repository.New(otherDB), Installations: installations.New(repository.New(otherDB), h.cfg.Auth.AccessSecret, discardLog()),
		Provider: &fakePush{}, Config: h.cfg.Push, Log: discardLog(), Clock: h.pushClock,
	})
	if err := otherDB.Reader().Close(); err != nil {
		t.Fatal(err)
	}
	broken.Tick(context.Background())
	if got := h.text(`SELECT status || ':' || error_code FROM notification_deliveries WHERE notification_id = ?`,
		res.NotificationID); got != "retrying:notification_lookup_failed" {
		t.Fatalf("after a failed read: %s", got)
	}
	h.pushClock.Advance(time.Minute)
	h.tick()
	if got := h.text(`SELECT status FROM notification_deliveries WHERE notification_id = ?`, res.NotificationID); got != domain.DeliveryAccepted {
		t.Fatalf("after the retry: %s", got)
	}
}

// Автоматты хабарлама (сатып алу) үлкен науқанның артында күтпейді.
func TestAutomaticNotificationsGoBeforeACampaignBacklog(t *testing.T) {
	h := newHarness(t, withEnv("PUSH_BATCH_SIZE", "1"))
	buyer := h.signIn("buyer@example.com")
	h.mustRegister(installation(installID(89), "android", fcmToken(89)), buyer.access)
	for i := 0; i < 3; i++ {
		s := h.signIn(fmt.Sprintf("audience-%d@example.com", i))
		h.mustRegister(installation(installID(890+i), "ios", fcmToken(890+i)), s.access)
	}
	admin := h.signInAdmin()
	if res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", map[string]any{
		"title": map[string]string{"ru": "Новость"}, "body": map[string]string{"ru": "Новая функция"}, "send": true,
	}, withKey(admin.headers(h.cfg.Admin.CookieName), "campaign-backlog-0001")); res.status != http.StatusCreated {
		t.Fatalf("campaign: %d %s", res.status, res.raw)
	}

	// While the first campaign push is being sent, a purchase happens.
	var once sync.Once
	var notifyErr error
	h.push.whenSending(func() {
		once.Do(func() {
			h.pushClock.Advance(time.Second)
			_, notifyErr = h.notify.NotifyUser(context.Background(), accountNotice(buyer.userID, "priority:1"))
		})
	})
	h.tick()
	h.push.whenSending(nil)
	if notifyErr != nil {
		t.Fatal(notifyErr)
	}
	var order []string
	for _, m := range h.push.messages() {
		order = append(order, m.msg.Data["type"])
	}
	if len(order) != 5 || order[0] != domain.TypeCampaign || order[1] != "account_notice" {
		t.Fatalf("send order = %v", order)
	}
}

// Екі процестің жұмысшылары бір дерекқорда: әр жеткізу бір-ақ рет жіберіледі.
func TestConcurrentWorkersNeverSendTheSameDeliveryTwice(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	const devices = 60
	for i := 0; i < devices; i++ {
		s := h.signIn(fmt.Sprintf("crowd-%03d@example.com", i))
		platform := domain.PlatformAndroid
		if i%2 == 1 {
			platform = domain.PlatformIOS
		}
		if _, err := h.installations.Register(ctx, installations.Registration{
			InstallationID: installID(1000 + i), Platform: platform, AppVersion: "1.3.2",
			Push: &installations.PushToken{Token: fcmToken(1000 + i)},
		}, s.userID); err != nil {
			t.Fatal(err)
		}
	}
	admin := h.signInAdmin()
	res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", map[string]any{
		"title": map[string]string{"ru": "Новость"}, "body": map[string]string{"ru": "Новая функция"}, "send": true,
	}, withKey(admin.headers(h.cfg.Admin.CookieName), "campaign-concurrency-0001"))
	if res.status != http.StatusCreated {
		t.Fatalf("campaign: %d %s", res.status, res.raw)
	}

	// A second "process": its own connection pool, provider and owner id.
	otherDB, err := database.Open(database.Options{Path: h.dbPath, MaxReadConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer otherDB.Close()
	otherPush := &fakePush{}
	other := notifications.New(notifications.Deps{
		Repo: repository.New(otherDB), Installations: installations.New(repository.New(otherDB), h.cfg.Auth.AccessSecret, discardLog()),
		Provider: otherPush, Config: h.cfg.Push, Log: discardLog(), Clock: h.pushClock,
	})

	var wg sync.WaitGroup
	for _, worker := range []*notifications.Service{h.notify, other} {
		wg.Add(1)
		go func(w *notifications.Service) {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				w.Tick(ctx)
			}
		}(worker)
	}
	wg.Wait()

	counts := map[string]int{}
	for _, p := range []*fakePush{h.push, otherPush} {
		for token, n := range sentTokens(p) {
			counts[token] += n
		}
	}
	if len(counts) != devices {
		t.Fatalf("%d devices received the campaign, want %d", len(counts), devices)
	}
	for token, n := range counts {
		if n != 1 {
			t.Fatalf("token %s… received %d copies", token[:14], n)
		}
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE status = 'provider_accepted'`); n != devices {
		t.Fatalf("accepted = %d", n)
	}
	if got := h.text(`SELECT status FROM notification_campaigns WHERE id = ?`, res.str("id")); got != domain.CampaignCompleted {
		t.Fatalf("campaign status = %s", got)
	}
}

// Санат өшірілсе, маркетинг келмейді; қауіпсіздік хабарламасы бәрібір келеді.
func TestPreferencesSilenceOptionalCategoriesOnly(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("prefs@example.com")
	h.mustRegister(installation(installID(16), "android", fcmToken(16)), s.access)

	res := h.do(http.MethodPut, "/api/v1/me/notification-preferences",
		map[string]any{"preferences": map[string]bool{"marketing": false}}, h.auth(s.access))
	if res.status != http.StatusOK {
		t.Fatalf("preferences: %d %s", res.status, res.raw)
	}
	for _, body := range []map[string]any{
		{"preferences": map[string]bool{"security": false}},
		{"preferences": map[string]bool{"promo": false}},
		{"preferences": map[string]bool{}},
	} {
		if bad := h.do(http.MethodPut, "/api/v1/me/notification-preferences", body, h.auth(s.access)); bad.status != http.StatusBadRequest {
			t.Fatalf("%v: %d", body, bad.status)
		}
	}
	ctx := context.Background()
	text := func(string) (string, string) { return "Акция", "Жеңілдік" }
	marketing, err := h.notify.NotifyUser(ctx, notifications.UserNotification{UserID: s.userID, IdempotencyKey: "m:1",
		Type: "promo", Category: domain.CategoryMarketing, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	security, err := h.notify.NotifyUser(ctx, notifications.UserNotification{UserID: s.userID, IdempotencyKey: "s:1",
		Type: "new_sign_in", Category: domain.CategorySecurity, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	if marketing.Devices != 0 || marketing.Skipped != notifications.SkipDisabledByUser || security.Devices != 1 {
		t.Fatalf("marketing %+v, security %+v", marketing, security)
	}
	get := h.do(http.MethodGet, "/api/v1/me/notification-preferences", nil, h.auth(s.access))
	prefs := get.body["preferences"].(map[string]any)
	if prefs["marketing"] != false || prefs["security"] != true || prefs["subscription"] != true {
		t.Fatalf("preferences = %v", prefs)
	}
	if optional := get.body["optional"].([]any); len(optional) != 4 {
		t.Fatalf("optional = %v", optional)
	}
}

// Рұқсат жоқ не қосымшада өшірілген құрылғыға кезек қойылмайды.
func TestPermissionAndInAppSwitchAreRespected(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("denied@example.com")
	denied := installation(installID(17), "ios", fcmToken(17))
	denied["notification_permission"] = "denied"
	h.mustRegister(denied, s.access)
	off := installation(installID(18), "android", fcmToken(18))
	off["notifications_enabled"] = false
	h.mustRegister(off, s.access)
	if res := h.notifyUser(s.userID, "perm:1"); res.Devices != 0 || res.Skipped != notifications.SkipNoDevices {
		t.Fatalf("queued %d deliveries without permission (%+v)", res.Devices, res)
	}
}

// Push өшірулі: оқиға бәрібір жазылады, push жолы skipped (push_disabled), қате жоқ.
func TestNotifyUserRecordsASkippedPushWhenPushIsOff(t *testing.T) {
	h := newHarness(t, withEnv("PUSH_NOTIFICATIONS_ENABLED", "false"))
	s := h.signIn("push-off@example.com")
	h.mustRegister(installation(installID(69), "android", fcmToken(69)), s.access)
	res, err := h.notify.NotifyUser(context.Background(), accountNotice(s.userID, "off:1"))
	if err != nil || res.Skipped != notifications.SkipPushDisabled || !res.Created || res.Devices != 0 {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	var status, code, channel string
	var device sql.NullString
	if err := h.db.Reader().QueryRow(`SELECT status, error_code, channel, installation_id FROM notification_deliveries
		WHERE notification_id = ?`, res.NotificationID).Scan(&status, &code, &channel, &device); err != nil {
		t.Fatal(err)
	}
	if status != domain.DeliverySkipped || code != notifications.SkipPushDisabled || channel != domain.ChannelPush || device.Valid {
		t.Fatalf("skipped row = %s %s %s %v", status, code, channel, device)
	}
	// Repeating the event changes nothing.
	again, err := h.notify.NotifyUser(context.Background(), accountNotice(s.userID, "off:1"))
	if err != nil || again.Created || again.NotificationID != res.NotificationID {
		t.Fatalf("again = %+v %v", again, err)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notification_deliveries`); n != 1 {
		t.Fatalf("deliveries = %d", n)
	}
	h.tick()
	if len(h.push.messages()) != 0 {
		t.Fatal("nothing is sent with push off")
	}

	cfg := h.do(http.MethodGet, "/api/v1/config", nil, nil)
	features := cfg.body["features"].(map[string]any)
	if features["push_notifications"] != false || features["installations"] != true || features["preferred_language"] != true {
		t.Fatalf("features: %v", features)
	}
	if reg := h.mustRegister(installation(installID(69), "android", fcmToken(69)), s.access); reg.body["push_available"] != false {
		t.Fatalf("push_available: %s", reg.raw)
	}
	admin := h.signInAdmin()
	send := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", map[string]any{
		"title": map[string]string{"ru": "t"}, "body": map[string]string{"ru": "b"}, "send": true,
	}, withKey(admin.headers(h.cfg.Admin.CookieName), "push-off-campaign-01"))
	if send.status != http.StatusConflict || send.errorCode() != "PUSH_DISABLED" {
		t.Fatalf("send with push off: %d %s", send.status, send.raw)
	}
	meta := h.do(http.MethodGet, "/api/v1/admin/notifications", nil, admin.headers(h.cfg.Admin.CookieName))
	if meta.body["status"].(map[string]any)["enabled"] != false {
		t.Fatalf("status: %s", meta.raw)
	}
}

// Симулятор, ескі install-token және өшірілген тіркелгі ешқашан хабарлама алмайды; ештеңе жазылмайды.
func TestExcludedAccountsAreNeverNotified(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.db.Writer().ExecContext(ctx, `INSERT INTO users (id, email, status, kind, created_at, updated_at)
		VALUES ('sim-user-0001', 'sim@demo.local', 'active', 'simulator', 1000, 1000)`); err != nil {
		t.Fatal(err)
	}
	disabled := h.signIn("disabled@example.com")
	if err := h.store.UpdateUserStatus(ctx, disabled.userID, domain.UserDisabled); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"sim-user-0001", disabled.userID} {
		res, err := h.notify.NotifyUser(ctx, accountNotice(id, "excluded:1"))
		if err != nil || res.Skipped != notifications.SkipRecipientExcluded || res.NotificationID != "" {
			t.Fatalf("%s: %+v %v", id, res, err)
		}
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notifications`); n != 0 {
		t.Fatalf("notifications = %d", n)
	}
}

// Қосымша push ашылғанын хабарлайды: тек сол құрылғыға жіберілген жеткізу белгіленеді.
func TestNotificationOpenedIsRecordedFromTheReceivingDevice(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("opener@example.com")
	h.mustRegister(installation(installID(62), "ios", fcmToken(62)), s.access)
	h.mustRegister(installation(installID(63), "android", fcmToken(63)), "")
	res := h.notifyUser(s.userID, "open:1")
	h.tick()
	did := h.text(`SELECT id FROM notification_deliveries WHERE notification_id = ?`, res.NotificationID)
	if msg := h.push.messages(); len(msg) != 1 || msg[0].msg.Data["did"] != did {
		t.Fatalf("the payload carries the delivery id: %+v", msg)
	}

	// Another installation cannot mark it.
	other := h.do(http.MethodPost, "/api/v1/notifications/opened",
		map[string]any{"installation_id": installID(63), "delivery_id": did}, nil)
	if other.status != http.StatusOK || other.body["recorded"] != false {
		t.Fatalf("other device: %d %s", other.status, other.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE id = ? AND opened_at IS NOT NULL`, did); n != 0 {
		t.Fatal("another device marked the delivery as opened")
	}
	for i := 0; i < 2; i++ {
		opened := h.do(http.MethodPost, "/api/v1/notifications/opened",
			map[string]any{"installation_id": installID(62), "delivery_id": did}, h.auth(s.access))
		if opened.status != http.StatusOK || opened.body["recorded"] != true || opened.body["ok"] != true {
			t.Fatalf("open: %d %s", opened.status, opened.raw)
		}
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE id = ? AND opened_at IS NOT NULL`, did); n != 1 {
		t.Fatal("the open was not recorded")
	}
	if bad := h.do(http.MethodPost, "/api/v1/notifications/opened",
		map[string]any{"installation_id": "x", "delivery_id": did}, nil); bad.status != http.StatusBadRequest ||
		bad.str("error", "details", "field") != "installation_id" {
		t.Fatalf("bad installation id: %d %s", bad.status, bad.raw)
	}
	if bad := h.do(http.MethodPost, "/api/v1/notifications/opened",
		map[string]any{"installation_id": installID(62), "delivery_id": did}, h.auth("expired")); bad.status != http.StatusUnauthorized {
		t.Fatalf("an invalid token is refused: %d", bad.status)
	}
}

// Әкімші тіркелгіні өшірсе не сессияларын жапса, құрылғылар тіркелгіден ажырайды.
func TestDisablingAnAccountOrRevokingSessionsDetachesItsDevices(t *testing.T) {
	h := newHarness(t)
	a := h.signIn("disable-me@example.com")
	b := h.signIn("revoke-me@example.com")
	h.mustRegister(installation(installID(90), "android", fcmToken(90)), a.access)
	h.mustRegister(installation(installID(91), "ios", fcmToken(91)), b.access)
	admin := h.signInAdmin().headers(h.cfg.Admin.CookieName)

	if res := h.do(http.MethodPost, "/api/v1/admin/users/"+a.userID+"/status", map[string]any{"status": "disabled"}, admin); res.status != http.StatusOK {
		t.Fatalf("disable: %d %s", res.status, res.raw)
	}
	if res := h.do(http.MethodPost, "/api/v1/admin/users/"+b.userID+"/revoke-sessions", map[string]any{}, admin); res.status != http.StatusOK {
		t.Fatalf("revoke: %d %s", res.status, res.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM app_installations WHERE user_id IS NOT NULL`); n != 0 {
		t.Fatalf("attached installations = %d", n)
	}
	entries, _, err := h.admin.AuditLog(context.Background(), traits.NewPage(50, 0))
	if err != nil {
		t.Fatal(err)
	}
	detached := 0
	for _, e := range entries {
		if v, ok := e.Metadata["devices_detached"].(float64); ok {
			detached += int(v)
		}
	}
	if detached != 2 {
		t.Fatalf("audit devices_detached = %d", detached)
	}
}
