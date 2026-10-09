package apptest

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/push"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// campaignBody — науқан формасы: kk, ru, en толтырылған, uz бос, қор тілі ru.
func campaignBody(extra map[string]any) map[string]any {
	body := map[string]any{
		"name":            "Күзгі жаңалық",
		"category":        "marketing",
		"fallback_locale": "ru",
		"title":           map[string]string{"kk": "Жаңалық", "ru": "Новость", "en": "News", "uz": ""},
		"body":            map[string]string{"kk": "Жаңа мүмкіндік", "ru": "Новая функция", "en": "A new feature"},
		"link":            "aireply://compose",
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

// optInMarketing — жарнама хабарламаларына келісім (қолданушы өзі қоспаса, науқан келмейді).
func (h *harness) optInMarketing(access string) {
	h.t.Helper()
	if res := h.do(http.MethodPut, "/api/v1/me/notification-preferences",
		map[string]any{"preferences": map[string]any{"marketing": true}}, h.auth(access)); res.status != http.StatusOK {
		h.t.Fatalf("marketing opt-in: %d %s", res.status, res.raw)
	}
}

func (h *harness) setPreferredLanguage(access, language string) {
	h.t.Helper()
	if res := h.do(http.MethodPatch, "/api/v1/me", map[string]any{"preferred_language": language}, h.auth(access)); res.status != http.StatusOK {
		h.t.Fatalf("preferred_language: %d %s", res.status, res.raw)
	}
}

// useQuota — бүгінгі (қолданба белдеуінде) жұмсалған генерация саны.
func (h *harness) useQuota(userID string, used int) {
	h.t.Helper()
	today := h.pushClock.Now().In(h.cfg.App.Location()).Format("2006-01-02")
	if _, err := h.db.Writer().Exec(`INSERT INTO usage_daily (user_id, usage_date, used, updated_at) VALUES (?,?,?,0)
		ON CONFLICT (user_id, usage_date) DO UPDATE SET used = excluded.used`, userID, today, used); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) planID(code string) string {
	h.t.Helper()
	return h.text(`SELECT id FROM plans WHERE code = ?`, code)
}

// Науқан: Idempotency-Key-сіз жоқ; сол кілтпен қайталау жаңа науқан ашпайды.
func TestCampaignCreationIsIdempotent(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("campaign-target@example.com")
	h.optInMarketing(s.access)
	h.mustRegister(installation(installID(21), "android", fcmToken(21)), s.access)
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	body := campaignBody(map[string]any{"send": true})

	if res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", body, headers); res.status != http.StatusBadRequest ||
		res.str("error", "details", "field") != "idempotency_key" {
		t.Fatalf("no Idempotency-Key: %d %s", res.status, res.raw)
	}
	first := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", body, withKey(headers, "campaign-key-0000001"))
	second := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", body, withKey(headers, "campaign-key-0000001"))
	if first.status != http.StatusCreated || second.status != http.StatusOK || first.str("id") != second.str("id") ||
		second.body["created"] != false || first.body["created"] != true {
		t.Fatalf("first %d %s / second %d %s", first.status, first.raw, second.status, second.raw)
	}
	if first.str("title", "kk") != "Жаңалық" || first.str("fallback_locale") != "ru" || first.str("status") != domain.CampaignQueued {
		t.Fatalf("dto: %s", first.raw)
	}
	// Sending again is harmless too.
	if res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns/"+first.str("id")+"/send", map[string]any{}, headers); res.status != http.StatusOK {
		t.Fatalf("second send: %d", res.status)
	}
	h.tick()
	h.tick()
	if n := len(h.push.messages()); n != 1 {
		t.Fatalf("the device received %d copies", n)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notification_campaigns`); n != 1 {
		t.Fatalf("campaigns = %d", n)
	}
	detail := h.do(http.MethodGet, "/api/v1/admin/notifications/campaigns/"+first.str("id"), nil, headers)
	if detail.str("status") != domain.CampaignCompleted || detail.num("stats", "provider_accepted") != 1 ||
		detail.num("stats", "by_language", "kk", "provider_accepted") != 1 || detail.body["errors"] == nil {
		t.Fatalf("campaign detail: %s", detail.raw)
	}
	entries, _, _ := h.admin.AuditLog(context.Background(), traits.NewPage(50, 0))
	actions := map[string]int{}
	for _, e := range entries {
		actions[e.Action]++
	}
	if actions["campaign.create"] != 1 || actions["campaign.send"] != 1 {
		t.Fatalf("audit = %v", actions)
	}
}

// Бір Idempotency-Key басқа мазмұнмен қайта келсе — 409, бірінші науқан өзгермейді.
func TestIdempotencyKeyCannotBeReusedForOtherContent(t *testing.T) {
	h := newHarness(t)
	admin := h.signInAdmin()
	headers := withKey(admin.headers(h.cfg.Admin.CookieName), "reused-key-00000001")
	first := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(nil), headers)
	if first.status != http.StatusCreated || first.str("created_by_email") != adminEmail || first.str("status") != domain.CampaignDraft {
		t.Fatalf("first: %d %s", first.status, first.raw)
	}
	again := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(nil), headers)
	if again.status != http.StatusOK || again.str("id") != first.str("id") {
		t.Fatalf("a retry returns the first campaign: %d %s", again.status, again.raw)
	}
	other := campaignBody(map[string]any{"body": map[string]string{"kk": "Басқа", "ru": "Новая функция", "en": "A new feature"}})
	conflict := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", other, headers)
	if conflict.status != http.StatusConflict || conflict.errorCode() != "CONFLICT" || conflict.str("error", "details", "field") != "idempotency_key" {
		t.Fatalf("different content under the same key: %d %s", conflict.status, conflict.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notification_campaigns WHERE idempotency_key = 'reused-key-00000001'`); n != 1 {
		t.Fatalf("campaigns = %d", n)
	}
}

// Сақталған жоба сол кілтпен жіберілсе — аудитте send бар.
func TestSendingASavedDraftByRetryIsAudited(t *testing.T) {
	h := newHarness(t)
	admin := h.signInAdmin()
	headers := withKey(admin.headers(h.cfg.Admin.CookieName), "draft-then-send-001")
	if res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(nil), headers); res.status != http.StatusCreated {
		t.Fatalf("draft: %d %s", res.status, res.raw)
	}
	sent := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{"send": true}), headers)
	if sent.status != http.StatusOK || sent.str("status") != domain.CampaignQueued || sent.body["created"] != false {
		t.Fatalf("send by retry: %d %s", sent.status, sent.raw)
	}
	for action, want := range map[string]int{"campaign.create": 1, "campaign.send": 1} {
		if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action = ?`, action); n != want {
			t.Errorf("%s = %d, want %d", action, n, want)
		}
	}
	again := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{"send": true}), headers)
	if again.status != http.StatusOK {
		t.Fatalf("second retry: %d", again.status)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action = 'campaign.send'`); n != 1 {
		t.Fatal("a retry of an already queued campaign is not another send")
	}
}

// Науқанды тоқтату: жіберілмегені тоқтайды, тоқтатылғанды жіберу — қақтығыс.
func TestCampaignCancel(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("cancel-target@example.com")
	h.mustRegister(installation(installID(40), "android", fcmToken(40)), s.access)
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	draft := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(nil), withKey(headers, "campaign-draft-000001"))
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
	if len(h.push.messages()) != 0 {
		t.Fatal("a cancelled campaign was sent")
	}
	if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action = 'campaign.cancel'`); n != 1 {
		t.Fatalf("cancel audits = %d", n)
	}
	list := h.do(http.MethodGet, "/api/v1/admin/notifications/campaigns?status=cancelled", nil, headers)
	if list.num("total") != 1 || len(list.body["campaigns"].([]any)) != 1 {
		t.Fatalf("list: %s", list.raw)
	}
	if res := h.do(http.MethodGet, "/api/v1/admin/notifications/campaigns?status=nope", nil, headers); res.status != http.StatusBadRequest {
		t.Fatalf("unknown status filter: %d", res.status)
	}
}

// Науқан жіберіліп жатқанда тоқтатылса, сол сәттегі жіберу кейін қайталанбайды.
func TestACancelledCampaignIsNotRetriedOrResent(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("cancel-in-flight@example.com")
	h.optInMarketing(s.access)
	h.mustRegister(installation(installID(41), "android", fcmToken(41)), s.access)
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	deliveryOf := func(campaignID string) string {
		t.Helper()
		return h.text(`SELECT status || ':' || error_code FROM notification_deliveries WHERE campaign_id = ?`, campaignID)
	}

	// The admin cancels while FCM is answering "unavailable".
	inFlight := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{"send": true}),
		withKey(headers, "cancel-in-flight-0001")).str("id")
	var cancelErr error
	h.push.whenSending(func() { _, _, cancelErr = h.notify.CancelCampaign(context.Background(), inFlight) })
	h.push.respond(push.Result{Outcome: push.Retry, Code: "UNAVAILABLE", StatusCode: 503})
	h.tick()
	h.push.whenSending(nil)
	if cancelErr != nil {
		t.Fatal(cancelErr)
	}
	if got := deliveryOf(inFlight); got != "cancelled:campaign_cancelled" {
		t.Fatalf("the send in flight: %s", got)
	}

	// A worker stopped mid-send; the campaign is cancelled before its lease runs out.
	crashed := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{"send": true}),
		withKey(headers, "cancel-in-flight-0002")).str("id")
	h.push.respond(push.Result{Outcome: push.Retry, Code: "UNAVAILABLE", StatusCode: 503})
	h.tick()
	if _, err := h.db.Writer().Exec(`UPDATE notification_deliveries SET status = 'sending', lease_owner = 'stopped-worker',
		lease_until = ? WHERE campaign_id = ?`, h.pushClock.Now().Add(time.Minute).UnixMilli(), crashed); err != nil {
		t.Fatal(err)
	}
	if res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns/"+crashed+"/cancel", map[string]any{}, headers); res.str("status") != domain.CampaignCancelled {
		t.Fatalf("cancel: %s", res.raw)
	}

	h.pushClock.Advance(time.Hour)
	h.tick()
	if got := deliveryOf(crashed); got != "cancelled:campaign_cancelled" {
		t.Fatalf("the abandoned send: %s", got)
	}
	if n := len(h.push.messages()); n != 2 {
		t.Fatalf("sends = %d, want only the two first attempts", n)
	}
}

// Тоқтатылған науқанның есебі оның жеткізулері өшірілгеннен кейін де қалады.
func TestACancelledCampaignKeepsItsTotals(t *testing.T) {
	h := newHarness(t, withEnv("PUSH_BATCH_SIZE", "1"))
	for i := 0; i < 2; i++ {
		s := h.signIn(fmt.Sprintf("cancel-totals-%d@example.com", i))
		h.optInMarketing(s.access)
		h.mustRegister(installation(installID(42+i), "android", fcmToken(42+i)), s.access)
	}
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	id := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{"send": true}),
		withKey(headers, "cancel-totals-000001")).str("id")
	var cancelErr error
	var once sync.Once
	h.push.whenSending(func() {
		once.Do(func() { _, _, cancelErr = h.notify.CancelCampaign(context.Background(), id) })
	})
	h.tick()
	h.push.whenSending(nil)
	if cancelErr != nil {
		t.Fatal(cancelErr)
	}
	totals := func() string {
		t.Helper()
		res := h.do(http.MethodGet, "/api/v1/admin/notifications/campaigns/"+id, nil, headers)
		return fmt.Sprintf("%s total=%v accepted=%v cancelled=%v", res.str("status"), res.num("stats", "total"),
			res.num("stats", "provider_accepted"), res.num("stats", "cancelled"))
	}
	want := "cancelled total=2 accepted=1 cancelled=1"
	if got := totals(); got != want {
		t.Fatalf("before retention: %s", got)
	}
	h.pushClock.Advance(time.Duration(h.cfg.Push.RetentionDays+1) * 24 * time.Hour)
	if deliveries, _ := h.notify.Retain(context.Background()); deliveries != 2 {
		t.Fatalf("retention removed %d deliveries", deliveries)
	}
	if got := totals(); got != want {
		t.Fatalf("after retention: %s", got)
	}
}

// Сол кілтпен қайталанған «жасау және жіберу» жіберу шегін жұмсамайды және бірінші жауапты алады.
func TestARetriedSendDoesNotUseTheSendBudget(t *testing.T) {
	h := newHarness(t, withEnv("RATE_PUSH_CAMPAIGNS_PER_HOUR", "1"))
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	body := campaignBody(map[string]any{"send": true})
	first := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", body, withKey(headers, "send-budget-000001"))
	again := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", body, withKey(headers, "send-budget-000001"))
	if first.status != http.StatusCreated || again.status != http.StatusOK || again.body["created"] != false ||
		again.str("id") != first.str("id") {
		t.Fatalf("first %d %s / retry %d %s", first.status, first.raw, again.status, again.raw)
	}
	other := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", body, withKey(headers, "send-budget-000002"))
	if other.status != http.StatusTooManyRequests || other.errorCode() != "RATE_LIMITED" {
		t.Fatalf("a second campaign within the hour: %d %s", other.status, other.raw)
	}
}

// Аудитте аудиторияның қысқа түрі: нақты адамдардың поштасы мен идентификаторы емес, саны.
func TestCampaignAuditSummarizesTheAudience(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("audited-person@example.com")
	admin := h.signInAdmin()
	res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{
		"audience": map[string]any{"emails": []string{"audited-person@example.com"}, "user_ids": []string{s.userID},
			"platforms": []string{"ios"}, "segment": "paid"},
	}), withKey(admin.headers(h.cfg.Admin.CookieName), "audit-summary-000001"))
	if res.status != http.StatusCreated {
		t.Fatalf("campaign: %d %s", res.status, res.raw)
	}
	meta := h.text(`SELECT metadata FROM admin_audit_logs WHERE action = 'campaign.create'`)
	if strings.Contains(meta, "audited-person@example.com") || strings.Contains(meta, s.userID) ||
		!strings.Contains(meta, `"audience":{"people":2,"platforms":["ios"],"segment":"paid"}`) {
		t.Fatalf("audit metadata: %s", meta)
	}
}

// Жаңа әкімші маршруттары сессиясыз жабық; күй өзгертетіндері CSRF-сіз де жабық.
func TestNotificationAdminRoutesRequireASession(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{
		"/api/v1/admin/notifications", "/api/v1/admin/notifications/campaigns",
		"/api/v1/admin/notifications/deliveries", "/api/v1/admin/notifications/devices",
	} {
		if res := h.do(http.MethodGet, path, nil, nil); res.status != http.StatusUnauthorized {
			t.Errorf("%s: %d", path, res.status)
		}
	}
	admin := h.signInAdmin()
	noCSRF := map[string]string{"Cookie": h.cfg.Admin.CookieName + "=" + admin.cookie, "Idempotency-Key": "csrf-test-0000001"}
	if res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(nil), noCSRF); res.status != http.StatusForbidden {
		t.Fatalf("campaign without CSRF: %d", res.status)
	}
	if res := h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview", map[string]any{"audience": map[string]any{}}, noCSRF); res.status != http.StatusForbidden {
		t.Fatalf("preview without CSRF: %d", res.status)
	}
}

// Әкімші панелінің метадерегі: сүзгілер, тілдер, шектер.
func TestNotificationAdminMetadata(t *testing.T) {
	h := newHarness(t)
	admin := h.signInAdmin()
	meta := h.do(http.MethodGet, "/api/v1/admin/notifications", nil, admin.headers(h.cfg.Admin.CookieName))
	if meta.status != http.StatusOK {
		t.Fatalf("meta: %d %s", meta.status, meta.raw)
	}
	status := meta.body["status"].(map[string]any)
	if status["enabled"] != true || status["fcm"] != true || status["email"] != true || status["worker"] != true {
		t.Fatalf("status: %v", status)
	}
	join := func(key string) string {
		var out []string
		for _, v := range meta.body[key].([]any) {
			out = append(out, v.(string))
		}
		return strings.Join(out, ",")
	}
	for key, want := range map[string]string{
		"content_locales": "kk,ru,en,uz", "required_locales": "kk,ru,en", "languages": "kk,ru,en,uz",
		"segments": "all,free,paid,demo", "subscription": "active,expired",
		"quota": "has_remaining,near_exhaustion,exhausted", "platforms": "android,ios",
		"types": "subscription_activated,subscription_expiring,subscription_expired,quota_low,quota_exhausted",
	} {
		if got := join(key); got != want {
			t.Errorf("%s = %s, want %s", key, got, want)
		}
	}
	if meta.str("fallback_locale") != "ru" || meta.num("limits", "recipients") != 500 || meta.num("limits", "title") != 80 {
		t.Fatalf("meta: %s", meta.raw)
	}
}

// Құрылғылар тізімі: анонимді орнатулар да бар, идентификатор қысқартылған, токен тек белгісімен.
func TestDeviceListShowsAnonymousInstallationsAndFingerprintsOnly(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("device-list@example.com")
	h.mustRegister(installation(installID(80), "android", fcmToken(80)), s.access)
	h.mustRegister(installation(installID(81), "ios", fcmToken(81)), "")
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)

	all := h.do(http.MethodGet, "/api/v1/admin/notifications/devices", nil, headers)
	if all.status != http.StatusOK || all.num("total") != 2 {
		t.Fatalf("devices: %d %s", all.status, all.raw)
	}
	raw := string(all.raw)
	hash := push.TokenHash(fcmToken(80))
	if strings.Contains(raw, installID(80)) || strings.Contains(raw, installID(81)) {
		t.Fatal("full installation ids would let anyone detach the phone")
	}
	if strings.Contains(raw, fcmToken(80)) || strings.Contains(raw, hash) || !strings.Contains(raw, "fcm:"+hash[:8]) ||
		!strings.Contains(raw, "iPhone 16 Pro") || !strings.Contains(raw, "Samsung SM-S928B") || strings.Contains(raw, "device-list@example.com") {
		t.Fatalf("the list leaks a token, misses the fingerprint or shows the full e-mail: %s", raw)
	}
	for query, want := range map[string]float64{
		"?auth=anonymous": 1, "?auth=authenticated": 1, "?auth=anonymous&platform=android": 0,
		"?user_id=" + s.userID: 1, "?push_status=active": 2,
	} {
		if res := h.do(http.MethodGet, "/api/v1/admin/notifications/devices"+query, nil, headers); res.num("total") != want {
			t.Errorf("%s: %s", query, res.raw)
		}
	}
	for _, query := range []string{"?auth=robots", "?platform=web", "?push_status=dead"} {
		if res := h.do(http.MethodGet, "/api/v1/admin/notifications/devices"+query, nil, headers); res.status != http.StatusBadRequest {
			t.Errorf("%s: %d", query, res.status)
		}
	}
}

// Құрылғы іздеуі орнату идентификаторын тек қысқа префиксі бойынша табады; алдын ала санау шектелген.
func TestInstallationIDCannotBeRecoveredBySearch(t *testing.T) {
	h := newHarness(t)
	h.mustRegister(installation(installID(86), "ios", fcmToken(86)), "")
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
	var last response
	for i := 0; i < 31; i++ {
		last = h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview", map[string]any{"audience": map[string]any{}}, admin)
	}
	if last.status != http.StatusTooManyRequests || last.num("error", "details", "retry_after_seconds") < 1 {
		t.Fatalf("31st preview in a minute: %d %s", last.status, last.raw)
	}
}

// Науқан әр алушыға өз тілінде: preferred_language → құрылғы тілі; бос тілге — қор тіл.
func TestCampaignIsSentInEachRecipientsLanguage(t *testing.T) {
	h := newHarness(t)
	type person struct {
		identifier, preferred, deviceLocale, wantTitle string
		seed                                           int
	}
	people := []person{
		{"ru-choice@example.com", "ru", "kk", "Новость", 101}, // the person's choice beats the device
		{"kk-device@example.com", "", "kk", "Жаңалық", 102},   // device language
		{"en-device@example.com", "", "en-US", "News", 103},   // device language, region dropped
		{"uz-choice@example.com", "uz", "uz", "Новость", 104}, // uz is empty: the fallback (ru)
		{"de-device@example.com", "", "de", "Жаңалық", 105},   // unsupported device language: account locale (kk)
	}
	for _, p := range people {
		s := h.signIn(p.identifier)
		h.optInMarketing(s.access)
		if p.preferred != "" {
			h.setPreferredLanguage(s.access, p.preferred)
		}
		body := installation(installID(p.seed), "android", fcmToken(p.seed))
		body["locale"] = p.deviceLocale
		h.mustRegister(body, s.access)
	}
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	created := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{"send": true}),
		withKey(headers, "campaign-languages-001"))
	if created.status != http.StatusCreated {
		t.Fatalf("campaign: %d %s", created.status, created.raw)
	}
	h.tick()
	h.tick()

	titles := map[string]string{}
	for _, m := range h.push.messages() {
		titles[m.token] = m.msg.Title
		if m.msg.Data["type"] != domain.TypeCampaign || m.msg.Data["link"] != "aireply://compose" || m.msg.Important {
			t.Fatalf("payload = %+v", m.msg)
		}
	}
	for _, p := range people {
		if got := titles[fcmToken(p.seed)]; got != p.wantTitle {
			t.Errorf("%s: %q, want %q", p.identifier, got, p.wantTitle)
		}
	}
	var locales []string
	rows, err := h.db.Reader().Query(`SELECT locale FROM notifications WHERE campaign_id = ? ORDER BY locale`, created.str("id"))
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var l string
		_ = rows.Scan(&l)
		locales = append(locales, l)
	}
	rows.Close()
	if strings.Join(locales, ",") != "en,kk,ru,uz" {
		t.Fatalf("one notification per language with readers: %v", locales)
	}
	if got := h.text(`SELECT title FROM notifications WHERE campaign_id = ? AND locale = 'uz'`, created.str("id")); got != "Новость" {
		t.Fatalf("uz text = %q", got)
	}

	detail := h.do(http.MethodGet, "/api/v1/admin/notifications/campaigns/"+created.str("id"), nil, headers)
	for lang, want := range map[string]float64{"kk": 2, "ru": 1, "en": 1, "uz": 1} {
		if got := detail.num("stats", "by_language", lang, "provider_accepted"); got != want {
			t.Errorf("by_language %s = %v, want %v", lang, got, want)
		}
	}
	if detail.num("recipient_count") != 5 || detail.num("device_count") != 5 || detail.str("status") != domain.CampaignCompleted {
		t.Fatalf("detail: %s", detail.raw)
	}
	byLocale := h.do(http.MethodGet, "/api/v1/admin/notifications/deliveries?campaign_id="+created.str("id")+"&locale=kk", nil, headers)
	if byLocale.num("total") != 2 {
		t.Fatalf("deliveries by locale: %s", byLocale.raw)
	}
}

// Алдын ала санау тіл бойынша бөледі; табылмаған пошталар қайтарылады.
func TestAudiencePreviewCountsByLanguageAndReportsUnknownEmails(t *testing.T) {
	h := newHarness(t)
	kk := h.signIn("preview-kk@example.com")
	ru := h.signIn("preview-ru@example.com")
	h.optInMarketing(kk.access)
	h.optInMarketing(ru.access)
	h.setPreferredLanguage(ru.access, "ru")
	h.mustRegister(installation(installID(201), "android", fcmToken(201)), kk.access)
	h.mustRegister(installation(installID(202), "ios", fcmToken(202)), kk.access)
	h.mustRegister(installation(installID(203), "android", fcmToken(203)), ru.access)
	h.mustRegister(installation(installID(204), "android", fcmToken(204)), "") // anonymous: never an audience
	h.mustRegister(installation(installID(205), "ios", ""), ru.access)         // no token yet

	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	all := h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview",
		map[string]any{"audience": map[string]any{"segment": "all"}, "category": "marketing"}, headers)
	if all.status != http.StatusOK {
		t.Fatalf("preview: %d %s", all.status, all.raw)
	}
	for path, want := range map[string]float64{
		"users": 2, "devices": 3, "android": 2, "ios": 1, "matched_devices": 4,
	} {
		if got := all.num("preview", path); got != want {
			t.Errorf("%s = %v, want %v (%s)", path, got, want, all.raw)
		}
	}
	for lang, want := range map[string][2]float64{"kk": {1, 2}, "ru": {1, 1}, "en": {0, 0}, "uz": {0, 0}} {
		if all.num("preview", "by_language", lang, "users") != want[0] || all.num("preview", "by_language", lang, "devices") != want[1] {
			t.Errorf("by_language %s: %s", lang, all.raw)
		}
	}
	if all.str("audience", "segment") != "" {
		t.Fatalf("segment all is normalised away: %s", all.raw)
	}

	named := h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview", map[string]any{"audience": map[string]any{
		"emails": []string{"Preview-RU@example.com", "nobody@example.com"},
	}}, headers)
	unresolved, _ := named.body["preview"].(map[string]any)["unresolved"].([]any)
	if named.num("preview", "users") != 1 || named.num("preview", "by_language", "ru", "devices") != 1 ||
		len(unresolved) != 1 || unresolved[0] != "nobody@example.com" {
		t.Fatalf("named audience: %s", named.raw)
	}
	nobody := h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview", map[string]any{"audience": map[string]any{
		"emails": []string{"nobody@example.com"},
	}}, headers)
	if nobody.num("preview", "users") != 0 || nobody.num("preview", "devices") != 0 {
		t.Fatalf("unknown e-mails only must reach nobody, not everyone: %s", nobody.raw)
	}
	for _, bad := range []map[string]any{
		{"audience": map[string]any{"platforms": []string{"windows"}}},
		{"audience": map[string]any{"sql": "1=1"}},
		{"audience": map[string]any{}, "category": "promo"},
		{"audience": map[string]any{"plan_ids": []string{"00000000-0000-4000-8000-00000000dead"}}},
	} {
		if res := h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview", bad, headers); res.status != http.StatusBadRequest {
			t.Errorf("%v: %d %s", bad, res.status, res.raw)
		}
	}
	if len(h.push.messages()) != 0 {
		t.Fatal("a preview must not send anything")
	}
}

// Әр аудитория сүзгісі: сегмент, тариф, жазылым, платформа, тіл, квота, нақты адамдар.
//
// Each person has a different number of devices (2, 4, 8, 16), so the device
// count of a preview tells exactly who matched; the simulator's single device
// would show up as an odd count.
func TestAudienceFilters(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	seed := 300
	devices := func(access, platform, locale string, n int) {
		h.optInMarketing(access) // the previews below are for marketing
		for i := 0; i < n; i++ {
			seed++
			body := installation(installID(seed), platform, fcmToken(seed))
			body["locale"] = locale
			h.mustRegister(body, access)
		}
	}

	// free: the default plan, Russian devices, today's quota used up.
	free := h.signIn("filter-free@example.com")
	devices(free.access, "android", "ru", 2)
	h.useQuota(free.userID, 7)

	// paid: pro assigned by an admin, iPhones, nothing used today.
	paid := h.signIn("filter-paid@example.com")
	devices(paid.access, "ios", "kk", 4)
	if res := h.do(http.MethodPost, "/api/v1/admin/users/"+paid.userID+"/plan",
		map[string]any{"plan_id": h.planID("pro")}, headers); res.status != http.StatusOK {
		t.Fatalf("assign: %d %s", res.status, res.raw)
	}

	// demo: standard bought through the demo payment provider.
	demo := h.signIn("filter-demo@example.com")
	devices(demo.access, "android", "kk", 8)
	h.openStore("standard")
	checkout := h.do(http.MethodPost, "/api/v1/payments/checkout", map[string]any{"plan_id": h.planID("standard")}, h.auth(demo.access))
	if checkout.status != http.StatusOK {
		t.Fatalf("checkout: %d %s", checkout.status, checkout.raw)
	}
	if res := h.do(http.MethodPost, "/api/v1/payments/"+checkout.str("payment_id")+"/confirm", map[string]any{}, h.auth(demo.access)); res.status != http.StatusOK {
		t.Fatalf("confirm: %d %s", res.status, res.raw)
	}

	// expired: had standard, it ran out; back on the free limit with one generation left.
	expired := h.signIn("filter-expired@example.com")
	devices(expired.access, "android", "kk", 16)
	past := h.pushClock.Now().Add(-48 * time.Hour)
	if _, err := h.store.ReplaceSubscription(ctx, expired.userID, domain.Subscription{
		UserID: expired.userID, PlanID: h.planID("standard"), Status: domain.SubActive, Source: "admin",
		StartedAt: past.Add(-30 * 24 * time.Hour), ExpiresAt: &past,
	}); err != nil {
		t.Fatal(err)
	}
	h.useQuota(expired.userID, 6)

	// The simulator account never belongs to an audience, whatever its device says.
	now := h.pushClock.Now().UnixMilli()
	if _, err := h.db.Writer().ExecContext(ctx, `INSERT INTO users (id, email, status, kind, created_at, updated_at)
		VALUES ('sim-user-0002', 'sim2@demo.local', 'active', 'simulator', 1000, 1000)`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Writer().ExecContext(ctx, `INSERT INTO app_installations (id, installation_id, user_id, platform,
		push_provider, push_token_sealed, push_token_hash, push_status, first_seen_at, last_seen_at, created_at, updated_at)
		VALUES ('sim-inst', 'sim-installation-1', 'sim-user-0002', 'android', 'fcm', 'v1:x', 'hash-sim', 'active', ?, ?, ?, ?)`,
		now, now, now, now); err != nil {
		t.Fatal(err)
	}

	weights := []struct {
		name   string
		weight int
	}{{"free", 2}, {"paid", 4}, {"demo", 8}, {"expired", 16}}
	who := func(audience map[string]any) string {
		t.Helper()
		res := h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview",
			map[string]any{"audience": audience, "category": "marketing"}, headers)
		if res.status != http.StatusOK {
			t.Fatalf("preview %v: %d %s", audience, res.status, res.raw)
		}
		count := int(res.num("preview", "devices"))
		if count%2 == 1 {
			t.Fatalf("the simulator's device is in the audience %v: %s", audience, res.raw)
		}
		var names []string
		for _, w := range weights {
			if count&w.weight != 0 {
				names = append(names, w.name)
			}
		}
		if float64(len(names)) != res.num("preview", "users") {
			t.Fatalf("users and devices disagree for %v: %s", audience, res.raw)
		}
		sort.Strings(names)
		return strings.Join(names, ",")
	}
	for name, c := range map[string]struct {
		audience map[string]any
		want     string
	}{
		"everyone":             {map[string]any{}, "demo,expired,free,paid"},
		"segment free":         {map[string]any{"segment": "free"}, "expired,free"},
		"segment paid":         {map[string]any{"segment": "paid"}, "paid"},
		"segment demo":         {map[string]any{"segment": "demo"}, "demo"},
		"plan pro":             {map[string]any{"plan_ids": []string{h.planID("pro")}}, "paid"},
		"plan free":            {map[string]any{"plan_ids": []string{h.planID("free")}}, "expired,free"},
		"plans standard, pro":  {map[string]any{"plan_ids": []string{h.planID("standard"), h.planID("pro")}}, "demo,paid"},
		"subscription active":  {map[string]any{"subscription": "active"}, "demo,paid"},
		"subscription expired": {map[string]any{"subscription": "expired"}, "expired"},
		"ios":                  {map[string]any{"platforms": []string{"ios"}}, "paid"},
		"russian":              {map[string]any{"languages": []string{"ru"}}, "free"},
		"kazakh":               {map[string]any{"languages": []string{"kk"}}, "demo,expired,paid"},
		"quota exhausted":      {map[string]any{"quota": "exhausted"}, "free"},
		"quota near":           {map[string]any{"quota": "near_exhaustion"}, "expired"},
		"quota remaining":      {map[string]any{"quota": "has_remaining"}, "demo,paid"},
		"user ids":             {map[string]any{"user_ids": []string{free.userID, demo.userID}}, "demo,free"},
		"emails":               {map[string]any{"emails": []string{"filter-paid@example.com"}}, "paid"},
		"ids and emails":       {map[string]any{"user_ids": []string{free.userID}, "emails": []string{"filter-paid@example.com"}}, "free,paid"},
		"free and exhausted":   {map[string]any{"segment": "free", "quota": "exhausted"}, "free"},
		"paid android":         {map[string]any{"segment": "paid", "platforms": []string{"android"}}, ""},
	} {
		if got := who(c.audience); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}
