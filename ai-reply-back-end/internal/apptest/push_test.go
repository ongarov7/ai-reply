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

	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/installations"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
	"github.com/aireply/ai-reply-back-end/internal/push"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// ---------------------------------------------------------------- helpers

func apnsToken(seed int) string { return fmt.Sprintf("%064x", seed) }

func fcmToken(seed int) string {
	return fmt.Sprintf("fcm-token-%04d:APA91bHPRgkF3JUikC4ENAHEeMrd41Zxv3hVZjC9KtT8OvPVGJ", seed)
}

func installID(seed int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", seed) }

// installation — POST /api/v1/installations денесі.
func installation(id, platform, token string) map[string]any {
	body := map[string]any{
		"installation_id": id, "platform": platform, "app_version": "1.3.2", "app_build": "142",
		"os_version": "17.5", "device_model": "iPhone17,1", "locale": "kk", "timezone": "Asia/Almaty",
		"notification_permission": "authorized", "notifications_enabled": true,
	}
	if platform == domain.PlatformAndroid {
		body["device_model"], body["manufacturer"], body["os_version"] = "SM-S928B", "samsung", "15"
	}
	if token != "" {
		body["push"] = map[string]any{"token": token, "environment": "production"}
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

func (h *harness) notifyUser(userID, key string) notifications.NotifyResult {
	h.t.Helper()
	res, err := h.notify.NotifyUser(context.Background(), notifications.UserNotification{
		UserID: userID, IdempotencyKey: key, Type: "account_notice", Category: domain.CategoryAccount,
		Title: "Тіркелгі", Body: "Хабарлама мәтіні", Link: "aireply://settings",
	})
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

// ---------------------------------------------------------------- installations

// Кірмеген қосымша да тіркеледі; қайта тіркеу жаңа жол ашпайды.
func TestAnonymousInstallationIsRegisteredOnce(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 3; i++ {
		res := h.mustRegister(installation(installID(1), "android", fcmToken(1)), "")
		if res.body["attached"] != false || res.str("push_status") != domain.PushActive {
			t.Fatalf("anonymous registration: %s", res.raw)
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
	h.mustRegister(installation(installID(2), "ios", apnsToken(2)), "")
	s := h.signIn("anon-to-auth@example.com")
	res := h.mustRegister(installation(installID(2), "ios", apnsToken(2)), s.access)
	if res.body["attached"] != true {
		t.Fatalf("authenticated registration must attach: %s", res.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM app_installations`); n != 1 {
		t.Fatalf("installations = %d, want the same row", n)
	}
	if got := h.text(`SELECT user_id FROM app_installations WHERE installation_id = ?`, installID(2)); got != s.userID {
		t.Fatalf("user_id = %q, want %q", got, s.userID)
	}
	if res.body["preferences"] == nil {
		t.Fatal("an attached installation gets the account's preferences")
	}
}

// Қосымша body-дегі user_id-ге ешқашан сенбейміз: ондай өріс қабылданбайды.
func TestInstallationNeverTakesAUserFromTheBody(t *testing.T) {
	h := newHarness(t)
	victim := h.signIn("victim@example.com")
	body := installation(installID(3), "ios", apnsToken(3))
	body["user_id"] = victim.userID
	if res := h.register(body, ""); res.status != http.StatusBadRequest {
		t.Fatalf("user_id in the body must be refused: %d", res.status)
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
	h.mustRegister(installation(installID(5), "ios", apnsToken(5)), s.access)

	res := h.do(http.MethodPost, "/api/v1/auth/logout", map[string]any{"refresh_token": s.refresh},
		map[string]string{"X-Installation-ID": installID(5)})
	if res.status != http.StatusOK {
		t.Fatalf("logout: %d %s", res.status, res.raw)
	}
	if got := h.text(`SELECT user_id FROM app_installations WHERE installation_id = ?`, installID(5)); got != "" {
		t.Fatalf("still attached to %q after logout", got)
	}

	// The explicit detach call and an anonymous registration do the same.
	other := h.signIn("logout2@example.com")
	h.mustRegister(installation(installID(6), "ios", apnsToken(6)), other.access)
	if res := h.do(http.MethodPost, "/api/v1/installations/"+installID(6)+"/detach", map[string]any{}, h.auth(other.access)); res.status != http.StatusOK || res.body["detached"] != true {
		t.Fatalf("detach: %d %s", res.status, res.raw)
	}
	h.mustRegister(installation(installID(6), "ios", apnsToken(6)), other.access)
	h.mustRegister(installation(installID(6), "ios", apnsToken(6)), "")
	if got := h.text(`SELECT user_id FROM app_installations WHERE installation_id = ?`, installID(6)); got != "" {
		t.Fatal("an anonymous registration means the app is signed out")
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
	sent := sentTokens(h.fcm)
	if sent[fcmToken(72)] != 1 || sent[fcmToken(71)] != 0 {
		t.Fatalf("sent = %v", sent)
	}
}

// Бір токен бір орнатуда ғана: қайта орнатылған қосымша ескі жолдан токенді алады.
func TestADuplicateTokenMovesToTheNewInstallation(t *testing.T) {
	h := newHarness(t)
	old := h.signIn("old-install@example.com")
	h.mustRegister(installation(installID(8), "ios", apnsToken(8)), old.access)
	h.mustRegister(installation(installID(9), "ios", apnsToken(8)), "")

	if n := h.scalar(`SELECT COUNT(*) FROM app_installations WHERE push_token_hash = ?`, push.TokenHash(apnsToken(8))); n != 1 {
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
	h.mustRegister(installation(installID(10), "ios", apnsToken(10)), a.access)
	queued := h.notifyUser(a.userID, "private:1")
	if queued.Devices != 1 {
		t.Fatalf("queued = %+v", queued)
	}
	// Before the worker runs, A signs out and B signs in on the same phone.
	h.mustRegister(installation(installID(10), "ios", apnsToken(10)), b.access)
	h.tick()
	if len(h.apns.messages()) != 0 {
		t.Fatal("A's notification reached the phone B is signed in on")
	}
	if got := h.text(`SELECT error_code FROM notification_deliveries WHERE notification_id = ?`, queued.NotificationID); got != "recipient_changed" {
		t.Fatalf("delivery error = %q", got)
	}
	h.notifyUser(b.userID, "private:2")
	h.tick()
	if len(h.apns.messages()) != 1 {
		t.Fatal("B's own notification must be delivered")
	}
}

// ---------------------------------------------------------------- delivery

// Бір оқиға — бір хабарлама; қолданушының әр құрылғысына бір-бірден.
func TestNotifyUserIsIdempotentAndReachesEveryDevice(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("two-phones@example.com")
	h.mustRegister(installation(installID(11), "android", fcmToken(11)), s.access)
	h.mustRegister(installation(installID(12), "ios", apnsToken(12)), s.access)

	first := h.notifyUser(s.userID, "payment_success:pay-1")
	again := h.notifyUser(s.userID, "payment_success:pay-1")
	if !first.Created || first.Devices != 2 || again.Created || again.NotificationID != first.NotificationID || again.Devices != 0 {
		t.Fatalf("first = %+v, again = %+v", first, again)
	}
	h.tick()
	h.tick()
	fcm, apns := h.fcm.messages(), h.apns.messages()
	if len(fcm) != 1 || len(apns) != 1 {
		t.Fatalf("fcm %d, apns %d; want exactly one each", len(fcm), len(apns))
	}
	msg := apns[0].msg
	if msg.Data["nid"] != first.NotificationID || msg.Data["did"] == "" || msg.Data["link"] != "aireply://settings" ||
		msg.Data["category"] != domain.CategoryAccount || !msg.Important || msg.CollapseID != first.NotificationID {
		t.Fatalf("payload = %+v", msg)
	}
	if apns[0].environment != "production" {
		t.Fatalf("APNs environment = %q", apns[0].environment)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE status = 'provider_accepted' AND sent_at IS NOT NULL`); n != 2 {
		t.Fatalf("accepted deliveries = %d", n)
	}
}

// Уақытша қате: артқа шегініспен қайталанады, сосын жеткізіледі.
func TestTransientFailureIsRetriedWithBackoff(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("retry@example.com")
	h.mustRegister(installation(installID(13), "android", fcmToken(13)), s.access)
	h.fcm.respond(push.Result{Outcome: push.Retry, Code: "UNAVAILABLE", StatusCode: 503})
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
	if len(h.fcm.messages()) != 1 {
		t.Fatal("retried before the backoff elapsed")
	}
	h.pushClock.Advance(10 * time.Second)
	h.tick()
	if got := h.text(`SELECT status FROM notification_deliveries WHERE notification_id = ?`, res.NotificationID); got != domain.DeliveryAccepted {
		t.Fatalf("status after retry = %s", got)
	}
	if len(h.fcm.messages()) != 2 {
		t.Fatalf("sends = %d, want 2", len(h.fcm.messages()))
	}
}

// Қайталау шектеулі: 5 әрекеттен кейін тоқтайды.
func TestRetriesStopAfterMaxAttempts(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("always-failing@example.com")
	h.mustRegister(installation(installID(14), "android", fcmToken(14)), s.access)
	for i := 0; i < 10; i++ {
		h.fcm.respond(push.Result{Outcome: push.Retry, Code: "INTERNAL", StatusCode: 500})
	}
	res := h.notifyUser(s.userID, "retry:max")
	for i := 0; i < 8; i++ {
		h.tick()
		h.pushClock.Advance(time.Hour)
	}
	if got := h.text(`SELECT status FROM notification_deliveries WHERE notification_id = ?`, res.NotificationID); got != domain.DeliveryFailed {
		t.Fatalf("status = %s", got)
	}
	if n := len(h.fcm.messages()); n != h.cfg.Push.MaxAttempts {
		t.Fatalf("attempts = %d, want %d", n, h.cfg.Push.MaxAttempts)
	}
}

// Провайдер «токен өлі» десе: орнату токені өшеді, қайталау жоқ.
func TestInvalidTokenIsDeactivated(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("dead-token@example.com")
	h.mustRegister(installation(installID(15), "ios", apnsToken(15)), s.access)
	h.apns.respond(push.Result{Outcome: push.InvalidToken, Code: "Unregistered", StatusCode: 410})
	res := h.notifyUser(s.userID, "invalid:1")
	h.tick()
	h.pushClock.Advance(time.Hour)
	h.tick()
	if got := h.text(`SELECT status FROM notification_deliveries WHERE notification_id = ?`, res.NotificationID); got != domain.DeliveryInvalidToken {
		t.Fatalf("delivery = %s", got)
	}
	if len(h.apns.messages()) != 1 {
		t.Fatal("a dead token must not be retried")
	}
	var status, reason string
	var hash sql.NullString
	if err := h.db.Reader().QueryRow(`SELECT push_status, push_status_reason, push_token_hash FROM app_installations WHERE installation_id = ?`,
		installID(15)).Scan(&status, &reason, &hash); err != nil {
		t.Fatal(err)
	}
	if status != domain.PushInvalid || reason != "Unregistered" || hash.Valid {
		t.Fatalf("installation: %s %s %v", status, reason, hash)
	}
	if again := h.notifyUser(s.userID, "invalid:2"); again.Devices != 0 {
		t.Fatal("nothing may be queued for a deactivated token")
	}
	// The app registers a fresh token: push works again.
	h.mustRegister(installation(installID(15), "ios", apnsToken(151)), s.access)
	if again := h.notifyUser(s.userID, "invalid:3"); again.Devices != 1 {
		t.Fatal("a new token must reactivate the installation")
	}
}

// Екі процестің жұмысшылары бір дерекқорда: әр жеткізу бір-ақ рет жіберіледі.
func TestConcurrentWorkersNeverSendTheSameDeliveryTwice(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	const devices = 120
	for i := 0; i < devices; i++ {
		platform, token := domain.PlatformAndroid, fcmToken(1000+i)
		if i%2 == 1 {
			platform, token = domain.PlatformIOS, apnsToken(1000+i)
		}
		if _, err := h.installations.Register(ctx, installations.Registration{
			InstallationID: installID(1000 + i), Platform: platform, AppVersion: "1.3.2",
			Push: &installations.PushToken{Token: token},
		}, ""); err != nil {
			t.Fatal(err)
		}
	}
	admin := h.signInAdmin()
	res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", map[string]any{
		"title": "Жаңалық", "body": "Жаңа мүмкіндік", "send": true,
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
	otherFCM, otherAPNs := &fakePush{name: "fcm"}, &fakePush{name: "apns"}
	other := notifications.New(notifications.Deps{
		Repo: repository.New(otherDB), Installations: installations.New(repository.New(otherDB), h.cfg.Auth.AccessSecret, discardLog()),
		Providers: map[string]push.Provider{domain.PlatformAndroid: otherFCM, domain.PlatformIOS: otherAPNs},
		Config:    h.cfg.Push, Log: discardLog(), Clock: h.pushClock,
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
	for _, p := range []*fakePush{h.fcm, h.apns, otherFCM, otherAPNs} {
		for token, n := range sentTokens(p) {
			counts[token] += n
		}
	}
	if len(counts) != devices {
		t.Fatalf("%d devices received the campaign, want %d", len(counts), devices)
	}
	for token, n := range counts {
		if n != 1 {
			t.Fatalf("token %s… received %d copies", token[:12], n)
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
	if bad := h.do(http.MethodPut, "/api/v1/me/notification-preferences",
		map[string]any{"preferences": map[string]bool{"security": false}}, h.auth(s.access)); bad.status != http.StatusBadRequest {
		t.Fatalf("security must stay on: %d", bad.status)
	}
	ctx := context.Background()
	marketing, _ := h.notify.NotifyUser(ctx, notifications.UserNotification{UserID: s.userID, IdempotencyKey: "m:1",
		Type: "promo", Category: domain.CategoryMarketing, Title: "Акция", Body: "Жеңілдік"})
	security, _ := h.notify.NotifyUser(ctx, notifications.UserNotification{UserID: s.userID, IdempotencyKey: "s:1",
		Type: "new_sign_in", Category: domain.CategorySecurity, Title: "Жаңа кіру", Body: "Тіркелгіңізге кірді"})
	if marketing.Devices != 0 || security.Devices != 1 {
		t.Fatalf("marketing %d, security %d", marketing.Devices, security.Devices)
	}
	get := h.do(http.MethodGet, "/api/v1/me/notification-preferences", nil, h.auth(s.access))
	prefs := get.body["preferences"].(map[string]any)
	if prefs["marketing"] != false || prefs["security"] != true || prefs["subscription"] != true {
		t.Fatalf("preferences = %v", prefs)
	}
}

// Рұқсат жоқ не қосымшада өшірілген құрылғыға кезек қойылмайды.
func TestPermissionAndInAppSwitchAreRespected(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("denied@example.com")
	denied := installation(installID(17), "ios", apnsToken(17))
	denied["notification_permission"] = "denied"
	h.mustRegister(denied, s.access)
	off := installation(installID(18), "android", fcmToken(18))
	off["notifications_enabled"] = false
	h.mustRegister(off, s.access)
	if res := h.notifyUser(s.userID, "perm:1"); res.Devices != 0 {
		t.Fatalf("queued %d deliveries without permission", res.Devices)
	}
}

// Сәтті төлем — бір push, қайта растау екіншісін жібермейді.
func TestPaymentSuccessNotifiesOnce(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("payer@example.com")
	h.mustRegister(installation(installID(19), "ios", apnsToken(19)), s.access)
	plans := h.do(http.MethodGet, "/api/v1/plans", nil, nil)
	var planID string
	for _, p := range plans.body["plans"].([]any) {
		if plan := p.(map[string]any); plan["code"] == "pro" {
			planID = plan["id"].(string)
		}
	}
	checkout := h.do(http.MethodPost, "/api/v1/payments/checkout", map[string]any{"plan_id": planID}, h.auth(s.access))
	paymentID := checkout.str("payment_id")
	if checkout.status != http.StatusOK || paymentID == "" {
		t.Fatalf("checkout: %d %s", checkout.status, checkout.raw)
	}
	for i := 0; i < 2; i++ {
		if res := h.do(http.MethodPost, "/api/v1/payments/"+paymentID+"/confirm", map[string]any{}, h.auth(s.access)); res.status != http.StatusOK {
			t.Fatalf("confirm: %d %s", res.status, res.raw)
		}
	}
	h.tick()
	msgs := h.apns.messages()
	if len(msgs) != 1 || msgs[0].msg.Data["type"] != "payment_success" || msgs[0].msg.Data["link"] != "aireply://subscription" {
		t.Fatalf("messages = %+v", msgs)
	}
	if !strings.Contains(msgs[0].msg.Title, "Тариф") {
		t.Fatalf("the text must follow the account language (kk): %q", msgs[0].msg.Title)
	}
}

// Тариф бітуі туралы еске салу әр мерзімге бір рет.
func TestSubscriptionRemindersAreSentOncePerPeriod(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("expiring@example.com")
	h.mustRegister(installation(installID(20), "android", fcmToken(20)), s.access)
	ctx := context.Background()
	var planID string
	if err := h.db.Reader().QueryRow(`SELECT id FROM plans WHERE code = 'standard'`).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	expires := h.pushClock.Now().Add(48 * time.Hour)
	if _, err := h.store.ReplaceSubscription(ctx, s.userID, domain.Subscription{
		UserID: s.userID, PlanID: planID, Status: domain.SubActive, Source: "admin",
		StartedAt: h.pushClock.Now().Add(-28 * 24 * time.Hour), ExpiresAt: &expires,
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		h.events.SweepSubscriptions(ctx)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notifications WHERE type = 'subscription_expiring' AND user_id = ?`, s.userID); n != 1 {
		t.Fatalf("reminders = %d", n)
	}
	// After the end: one "ended" notice.
	h.pushClock.Advance(49 * time.Hour)
	for i := 0; i < 3; i++ {
		h.events.SweepSubscriptions(ctx)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notifications WHERE type = 'subscription_expired' AND user_id = ?`, s.userID); n != 1 {
		t.Fatalf("expiry notices = %d", n)
	}
}

// ---------------------------------------------------------------- admin

func withKey(headers map[string]string, key string) map[string]string {
	out := map[string]string{"Idempotency-Key": key}
	for k, v := range headers {
		out[k] = v
	}
	return out
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// Науқан: Idempotency-Key-сіз жоқ; сол кілтпен қайталау жаңа науқан ашпайды.
func TestCampaignCreationIsIdempotent(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("campaign-target@example.com")
	h.mustRegister(installation(installID(21), "android", fcmToken(21)), s.access)
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	body := map[string]any{"name": "Күзгі акция", "title": "Жаңалық", "body": "Мәтін", "link": "aireply://subscription", "send": true}

	if res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", body, headers); res.status != http.StatusBadRequest {
		t.Fatalf("no Idempotency-Key: %d", res.status)
	}
	first := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", body, withKey(headers, "campaign-key-0000001"))
	second := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", body, withKey(headers, "campaign-key-0000001"))
	if first.status != http.StatusCreated || second.status != http.StatusOK || first.str("id") != second.str("id") ||
		second.body["created"] != false {
		t.Fatalf("first %d %s / second %d %s", first.status, first.raw, second.status, second.raw)
	}
	// Sending again is harmless too.
	if res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns/"+first.str("id")+"/send", map[string]any{}, headers); res.status != http.StatusOK {
		t.Fatalf("second send: %d", res.status)
	}
	h.tick()
	h.tick()
	if n := len(h.fcm.messages()); n != 1 {
		t.Fatalf("the device received %d copies", n)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notification_campaigns`); n != 1 {
		t.Fatalf("campaigns = %d", n)
	}
	detail := h.do(http.MethodGet, "/api/v1/admin/notifications/campaigns/"+first.str("id"), nil, headers)
	if detail.str("status") != domain.CampaignCompleted || detail.num("stats", "provider_accepted") != 1 {
		t.Fatalf("campaign detail: %s", detail.raw)
	}
	entries, _, _ := h.admin.AuditLog(context.Background(), traits.NewPage(50, 0))
	actions := map[string]int{}
	for _, e := range entries {
		actions[e.Action]++
	}
	if actions["notification.campaign.create"] != 1 || actions["notification.campaign.send"] != 1 {
		t.Fatalf("audit = %v", actions)
	}
}

// Алдын ала санау: сүзгілер серверде, ешнәрсе жіберілмейді.
func TestAudiencePreviewUsesServerSideFilters(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	paid := h.signIn("paid@example.com")
	free := h.signIn("free@example.com")
	var proID string
	if err := h.db.Reader().QueryRow(`SELECT id FROM plans WHERE code = 'pro'`).Scan(&proID); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(20 * 24 * time.Hour)
	if _, err := h.store.ReplaceSubscription(ctx, paid.userID, domain.Subscription{UserID: paid.userID, PlanID: proID,
		Status: domain.SubActive, Source: "admin", StartedAt: time.Now(), ExpiresAt: &expires}); err != nil {
		t.Fatal(err)
	}
	h.mustRegister(installation(installID(30), "ios", apnsToken(30)), paid.access)
	h.mustRegister(installation(installID(31), "android", fcmToken(31)), paid.access)
	h.mustRegister(installation(installID(32), "android", fcmToken(32)), free.access)
	ru := installation(installID(33), "android", fcmToken(33))
	ru["locale"] = "ru"
	h.mustRegister(ru, "")
	h.mustRegister(installation(installID(34), "ios", ""), "") // no token yet

	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	preview := func(audience map[string]any) response {
		res := h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview",
			map[string]any{"audience": audience, "category": "marketing"}, headers)
		if res.status != http.StatusOK {
			t.Fatalf("preview %v: %d %s", audience, res.status, res.raw)
		}
		return res
	}
	for name, c := range map[string]struct {
		audience       map[string]any
		devices, users float64
	}{
		"everyone":      {map[string]any{}, 4, 2},
		"paid":          {map[string]any{"payment": "paid"}, 2, 1},
		"unpaid":        {map[string]any{"payment": "unpaid"}, 1, 1},
		"paid ios":      {map[string]any{"payment": "paid", "platforms": []string{"ios"}}, 1, 1},
		"anonymous":     {map[string]any{"auth": "anonymous"}, 1, 0},
		"authenticated": {map[string]any{"auth": "authenticated"}, 3, 2},
		"russian":       {map[string]any{"locales": []string{"ru"}}, 1, 0},
		"no plan ever":  {map[string]any{"subscription": "none"}, 1, 1},
		"one user":      {map[string]any{"user_ids": []string{free.userID}}, 1, 1},
		"new version":   {map[string]any{"app_version_min": "2.0.0"}, 0, 0},
	} {
		res := preview(c.audience)
		if res.num("preview", "devices") != c.devices || res.num("preview", "users") != c.users {
			t.Errorf("%s: %s", name, res.raw)
		}
	}
	all := preview(map[string]any{})
	if all.num("preview", "matched_devices") != 5 || all.num("preview", "anonymous_devices") != 1 ||
		all.num("preview", "android") != 3 || all.num("preview", "ios") != 1 {
		t.Fatalf("breakdown: %s", all.raw)
	}
	if bad := h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview",
		map[string]any{"audience": map[string]any{"platforms": []string{"windows"}}}, headers); bad.status != http.StatusBadRequest {
		t.Fatalf("unknown filter value: %d", bad.status)
	}
	if bad := h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview",
		map[string]any{"audience": map[string]any{"sql": "1=1"}}, headers); bad.status != http.StatusBadRequest {
		t.Fatalf("unknown filter field: %d", bad.status)
	}
	if len(h.fcm.messages())+len(h.apns.messages()) != 0 {
		t.Fatal("a preview must not send anything")
	}
}

// Науқанды тоқтату: жіберілмегені тоқтайды, аяқталғанын тоқтату — қақтығыс.
func TestCampaignCancel(t *testing.T) {
	h := newHarness(t)
	h.mustRegister(installation(installID(40), "android", fcmToken(40)), "")
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	draft := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns",
		map[string]any{"title": "Черновик", "body": "Мәтін"}, withKey(headers, "campaign-draft-000001"))
	if draft.str("status") != domain.CampaignDraft {
		t.Fatalf("draft: %s", draft.raw)
	}
	cancelled := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns/"+draft.str("id")+"/cancel", map[string]any{}, headers)
	if cancelled.str("status") != domain.CampaignCancelled {
		t.Fatalf("cancel: %s", cancelled.raw)
	}
	if res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns/"+draft.str("id")+"/send", map[string]any{}, headers); res.status != http.StatusConflict {
		t.Fatalf("sending a cancelled campaign: %d", res.status)
	}
	h.tick()
	if len(h.fcm.messages()) != 0 {
		t.Fatal("a cancelled campaign was sent")
	}
}

// viewer рөлі науқан жібере алмайды, диагностика мен журналдарды көрмейді.
func TestViewerRoleIsReadOnly(t *testing.T) {
	h := newHarness(t)
	hash, err := auth.HashPassword("viewer-password-long")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.CreateAdmin(context.Background(), domain.AdminUser{
		Email: "viewer@aireply.test", Name: "Viewer", PasswordHash: hash, Role: "viewer",
	}); err != nil {
		t.Fatal(err)
	}
	viewer := h.signInAdminAs("viewer@aireply.test", "viewer-password-long")
	headers := viewer.headers(h.cfg.Admin.CookieName)
	s := h.signIn("viewed@example.com")

	for path, want := range map[string]int{
		"/api/v1/admin/notifications/campaigns":            http.StatusOK,
		"/api/v1/admin/dashboard":                          http.StatusOK,
		"/api/v1/admin/users/" + s.userID + "/diagnostics": http.StatusForbidden,
		"/api/v1/admin/logs/events":                        http.StatusForbidden,
		"/api/v1/admin/logs/auth":                          http.StatusForbidden,
		"/api/v1/admin/audit":                              http.StatusForbidden,
	} {
		if res := h.do(http.MethodGet, path, nil, headers); res.status != want {
			t.Errorf("GET %s: %d, want %d", path, res.status, want)
		}
	}
	for path, body := range map[string]map[string]any{
		"/api/v1/admin/notifications/campaigns":       {"title": "t", "body": "b", "send": true},
		"/api/v1/admin/users/" + s.userID + "/status": {"status": "disabled"},
		"/api/v1/admin/settings/limits":               {"max_source_characters": 400},
	} {
		if res := h.do(http.MethodPost, path, body, withKey(headers, "viewer-attempt-000001")); res.status != http.StatusForbidden {
			t.Errorf("POST %s: %d, want 403", path, res.status)
		}
	}
	session := h.do(http.MethodGet, "/api/v1/admin/session", nil, headers)
	if !strings.Contains(string(session.raw), "notifications.read") || strings.Contains(string(session.raw), "notifications.send") {
		t.Fatalf("viewer permissions: %s", session.raw)
	}
}

// Жаңа әкімші маршруттары сессиясыз жабық.
func TestNotificationAdminRoutesRequireASession(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{
		"/api/v1/admin/notifications/campaigns", "/api/v1/admin/notifications/deliveries",
		"/api/v1/admin/notifications/devices", "/api/v1/admin/logs/events", "/api/v1/admin/ops",
	} {
		if res := h.do(http.MethodGet, path, nil, nil); res.status != http.StatusUnauthorized {
			t.Errorf("%s: %d", path, res.status)
		}
	}
	admin := h.signInAdmin()
	noCSRF := map[string]string{"Cookie": h.cfg.Admin.CookieName + "=" + admin.cookie, "Idempotency-Key": "csrf-test-0000001"}
	if res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", map[string]any{"title": "t", "body": "b"}, noCSRF); res.status != http.StatusForbidden {
		t.Fatalf("campaign without CSRF: %d", res.status)
	}
}

// Құрылғылар тізімі мен диагностикада токеннің өзі жоқ — тек белгісі.
func TestAdminSeesTokenFingerprintsOnly(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("fingerprint@example.com")
	h.mustRegister(installation(installID(50), "android", fcmToken(50)), s.access)
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	for _, path := range []string{"/api/v1/admin/notifications/devices", "/api/v1/admin/users/" + s.userID + "/diagnostics"} {
		res := h.do(http.MethodGet, path, nil, headers)
		if res.status != http.StatusOK {
			t.Fatalf("%s: %d %s", path, res.status, res.raw)
		}
		raw := string(res.raw)
		hash := push.TokenHash(fcmToken(50))
		if strings.Contains(raw, fcmToken(50)) || strings.Contains(raw, hash) || !strings.Contains(raw, "fcm:"+hash[:8]) {
			t.Fatalf("%s leaks or misses the fingerprint: %s", path, raw)
		}
	}
	diag := h.do(http.MethodGet, "/api/v1/admin/users/"+s.userID+"/diagnostics", nil, headers)
	if diag.str("user", "email") != "fingerprint@example.com" {
		t.Fatalf("an authorised admin sees the e-mail: %s", diag.raw)
	}
	entries, _, _ := h.admin.AuditLog(context.Background(), traits.NewPage(50, 0))
	found := false
	for _, e := range entries {
		found = found || (e.Action == "user.diagnostics.view" && e.EntityID == s.userID && e.RequestID != "")
	}
	if !found {
		t.Fatal("opening diagnostics must leave an audit record with the request id")
	}
}
