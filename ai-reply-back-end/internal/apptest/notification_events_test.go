package apptest

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/email"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/push"
)

// ---------------------------------------------------------------- helpers

// notices — қолданушының осы түрдегі хабарламалар саны.
func (h *harness) notices(userID, kind string) int {
	h.t.Helper()
	return h.scalar(`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND type = ?`, userID, kind)
}

// noticeKeys — қолданушының автоматты хабарламаларының кілттері (реттелген).
func (h *harness) noticeKeys(userID string) []string {
	h.t.Helper()
	rows, err := h.db.Reader().Query(`SELECT idempotency_key FROM notifications WHERE user_id = ? ORDER BY idempotency_key`, userID)
	if err != nil {
		h.t.Fatal(err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			h.t.Fatal(err)
		}
		keys = append(keys, k)
	}
	return keys
}

// subscribe — тарифті дерекқорға тікелей жазады (ешқандай оқиғасыз).
func (h *harness) subscribe(userID, code string, expires time.Time) domain.Subscription {
	h.t.Helper()
	sub, err := h.store.ReplaceSubscription(context.Background(), userID, domain.Subscription{
		UserID: userID, PlanID: h.planID(code), Status: domain.SubActive, Source: "admin",
		StartedAt: h.clock.Now(), ExpiresAt: &expires,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return sub
}

// buy — демо төлем: checkout, содан кейін confirm (times рет). Төлем идентификаторын қайтарады.
func (h *harness) buy(s session, code string, times int) string {
	h.t.Helper()
	h.openStore(code)
	checkout := h.do(http.MethodPost, "/api/v1/payments/checkout", map[string]any{"plan_id": h.planID(code)}, h.auth(s.access))
	paymentID := checkout.str("payment_id")
	if checkout.status != http.StatusOK || paymentID == "" {
		h.t.Fatalf("checkout: %d %s", checkout.status, checkout.raw)
	}
	for i := 0; i < times; i++ {
		if res := h.do(http.MethodPost, "/api/v1/payments/"+paymentID+"/confirm", map[string]any{}, h.auth(s.access)); res.status != http.StatusOK {
			h.t.Fatalf("confirm: %d %s", res.status, res.raw)
		}
	}
	return paymentID
}

// assignPlan — әкімші панелі арқылы тариф беру.
func (h *harness) assignPlan(admin adminSession, userID, code, expires string) {
	h.t.Helper()
	body := map[string]any{"plan_id": h.planID(code)}
	if expires != "" {
		body["expires_at"] = expires
	}
	if res := h.do(http.MethodPost, "/api/v1/admin/users/"+userID+"/plan", body, admin.headers(h.cfg.Admin.CookieName)); res.status != http.StatusOK {
		h.t.Fatalf("assign plan: %d %s", res.status, res.raw)
	}
}

// pushesOf — осы түрдегі жіберілген push-тар.
func (h *harness) pushesOf(kind string) []push.Message {
	var out []push.Message
	for _, m := range h.push.messages() {
		if m.msg.Data["type"] == kind {
			out = append(out, m.msg)
		}
	}
	return out
}

func (h *harness) planName(code, locale string) string {
	h.t.Helper()
	return h.text(`SELECT name_`+locale+` FROM plans WHERE code = ?`, code)
}

// ---------------------------------------------------------------- subscription activated

// Төлем расталды: бір push және бір хат, алушының тілінде; қайта растау ештеңе қоспайды.
func TestPaymentConfirmationNotifiesOnce(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("payer@example.com")
	h.setPreferredLanguage(s.access, "ru")
	h.mustRegister(installation(installID(601), "ios", fcmToken(601)), s.access)

	paymentID := h.buy(s, "pro", 2)
	h.tick()
	h.tick()

	if n := h.notices(s.userID, domain.TypeSubscriptionActivated); n != 1 {
		t.Fatalf("activation notices = %d", n)
	}
	if got := h.text(`SELECT idempotency_key FROM notifications WHERE user_id = ?`, s.userID); got != "subscription_activated:payment:"+paymentID {
		t.Fatalf("key = %q", got)
	}
	msgs := h.push.messages()
	if len(msgs) != 1 {
		t.Fatalf("pushes = %d", len(msgs))
	}
	msg := msgs[0].msg
	if msg.Data["type"] != domain.TypeSubscriptionActivated || msg.Data["category"] != domain.CategorySubscription ||
		msg.Data["link"] != "aireply://subscription" || msg.Title != "Подписка успешно активирована" ||
		!strings.Contains(msg.Body, "«"+h.planName("pro", "ru")+"»") || !strings.Contains(msg.Body, "50") {
		t.Fatalf("push = %+v", msg)
	}
	for _, k := range []string{"plan", "date", "limit"} {
		if _, leaked := msg.Data[k]; leaked {
			t.Fatalf("template params never reach the device: %v", msg.Data)
		}
	}
	mails := h.mail.messages()
	if len(mails) != 1 || mails[0].To != "payer@example.com" || mails[0].Content.Subject != "subscription_activated ru" ||
		mails[0].Content.Text != h.planName("pro", "ru") {
		t.Fatalf("mails = %+v", mails)
	}
	// The plan has a period, so the e-mail gets its end date in the app timezone.
	var params map[string]string
	if err := json.Unmarshal([]byte(h.text(`SELECT params FROM notifications WHERE user_id = ?`, s.userID)), &params); err != nil {
		t.Fatal(err)
	}
	if params["date"] != "09.04.2026" || params["limit"] != "50" {
		t.Fatalf("params = %v", params)
	}
}

// «Тариф және төлем» санатын өшірген адам push алмайды, бірақ сатып алуды растайтын хат бәрібір келеді.
func TestActivationEmailIgnoresTheCategorySwitch(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("receipt@example.com")
	h.mustRegister(installation(installID(603), "android", fcmToken(603)), s.access)
	if res := h.do(http.MethodPut, "/api/v1/me/notification-preferences",
		map[string]any{"preferences": map[string]bool{"subscription": false}}, h.auth(s.access)); res.status != http.StatusOK {
		t.Fatalf("preferences: %d %s", res.status, res.raw)
	}
	h.buy(s, "pro", 1)
	h.tick()
	if n := len(h.push.messages()); n != 0 {
		t.Fatalf("pushes = %d", n)
	}
	if mails := h.mail.messages(); len(mails) != 1 || mails[0].To != "receipt@example.com" {
		t.Fatalf("mails = %+v", mails)
	}
	if got := h.text(`SELECT group_concat(channel || ':' || status || ':' || error_code, ' ') FROM
		(SELECT channel, status, error_code FROM notification_deliveries ORDER BY channel)`); got != "email:provider_accepted: push:skipped:disabled_by_user" {
		t.Fatalf("deliveries: %s", got)
	}
}

// Әкімші ақылы тариф берсе — хабарлама; тегін тариф, тіркелу және симулятор — ешқашан.
func TestAdminPlanAssignmentNotifiesOnlyForPaidPlans(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("assigned@example.com")
	h.mustRegister(installation(installID(602), "android", fcmToken(602)), s.access)
	if n := h.scalar(`SELECT COUNT(*) FROM notifications`); n != 0 {
		t.Fatalf("signing up gives the free plan without a notification: %d", n)
	}
	admin := h.signInAdmin()

	h.assignPlan(admin, s.userID, "free", "")
	h.assignPlan(admin, s.userID, "pro", "2026-03-01") // an end date already in the past
	if n := h.scalar(`SELECT COUNT(*) FROM notifications`); n != 0 {
		t.Fatalf("neither a free plan nor an ended one is news: %d", n)
	}
	h.assignPlan(admin, s.userID, "standard", "2026-04-09")
	sub := h.text(`SELECT id FROM subscriptions WHERE user_id = ? AND status = 'active'`, s.userID)
	if keys := h.noticeKeys(s.userID); len(keys) != 1 || keys[0] != "subscription_activated:sub:"+sub {
		t.Fatalf("keys = %v", keys)
	}
	h.tick()
	if msgs := h.pushesOf(domain.TypeSubscriptionActivated); len(msgs) != 1 || msgs[0].Title != "Жазылым сәтті белсендірілді" {
		t.Fatalf("pushes = %+v", msgs)
	}

	// The simulator account gets plans through its own path: never a notification.
	sim := h.signInSimulator()
	if res := h.do(http.MethodPost, "/api/v1/simulator/account/plan", map[string]any{"plan_id": h.planID("pro")},
		sim.headers(h.cfg.Admin.CookieName)); res.status != http.StatusOK {
		t.Fatalf("simulator plan: %d %s", res.status, res.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notifications`); n != 1 {
		t.Fatalf("notifications = %d", n)
	}
}

// Хат шаблоны: алушының тілі, тариф атауы, мерзімі, лимиті және қосымшадағы орны.
func TestActivationEmailUsesTheLocalizedTemplate(t *testing.T) {
	h := newHarness(t)
	bundle, err := localization.Load()
	if err != nil {
		t.Fatal(err)
	}
	h.notify.WithEmail(h.mail, email.NotificationTemplates{Translate: bundle.T, Brand: "AI Reply"})
	s := h.signIn("template@example.com") // account locale kk
	h.assignPlan(h.signInAdmin(), s.userID, "standard", "2026-04-09")
	h.tick()

	mails := h.mail.messages()
	if len(mails) != 1 {
		t.Fatalf("mails = %d", len(mails))
	}
	c := mails[0].Content
	if c.Subject != "«"+h.planName("standard", "kk")+"» тарифі қосылды" {
		t.Fatalf("subject = %q", c.Subject)
	}
	for _, part := range []string{"Сәлеметсіз бе!", "күніне 30 жауапқа дейін", "Тариф 09.04.2026 дейін жарамды.", "«Тариф» бөлімінен",
		"тариф қосылғандықтан жіберілді"} {
		if !strings.Contains(c.Text, part) || !strings.Contains(c.HTML, part) {
			t.Fatalf("%q missing:\n%s", part, c.Text)
		}
	}
	if !strings.Contains(c.HTML, `lang="kk"`) || strings.Contains(c.Text+c.HTML, "{") {
		t.Fatalf("rendered html/text:\n%s", c.Text)
	}
}

// Автоматты хабарламалар тілі: preferred_language → соңғы құрылғының тілі → тіркелгі тілі.
func TestAutomaticNotificationsFollowTheRecipientsLanguage(t *testing.T) {
	h := newHarness(t)
	admin := h.signInAdmin()

	chosen := h.signIn("chosen-ru@example.com")
	h.mustRegister(installation(installID(611), "android", fcmToken(611)), chosen.access) // device kk
	h.setPreferredLanguage(chosen.access, "ru")

	device := h.signIn("device-en@example.com")
	english := installation(installID(612), "ios", fcmToken(612))
	english["locale"] = "en"
	h.mustRegister(english, device.access)

	account := h.signIn("account-kk@example.com") // no device: users.locale kk

	for _, tc := range []struct {
		s             session
		locale, title string
	}{
		{chosen, "ru", "Подписка успешно активирована"},
		{device, "en", "Subscription activated"},
		{account, "kk", "Жазылым сәтті белсендірілді"},
	} {
		h.assignPlan(admin, tc.s.userID, "pro", "")
		row := h.text(`SELECT locale || '|' || title || '|' || body FROM notifications WHERE user_id = ?`, tc.s.userID)
		parts := strings.SplitN(row, "|", 3)
		if parts[0] != tc.locale || parts[1] != tc.title || !strings.Contains(parts[2], h.planName("pro", tc.locale)) {
			t.Fatalf("%s: %q", tc.locale, row)
		}
	}
	if got := h.text(`SELECT d.status || ':' || d.error_code FROM notification_deliveries d
		JOIN notifications n ON n.id = d.notification_id WHERE n.user_id = ? AND d.channel = 'push'`, account.userID); got != "skipped:no_devices" {
		t.Fatalf("no device: %s", got)
	}
}

// Хат жіберілмесе де төлем сәтті; хабарлама жазылмаса да төлем сәтті.
func TestNotificationFailuresNeverAffectThePayment(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("unlucky@example.com")
	h.mustRegister(installation(installID(621), "android", fcmToken(621)), s.access)

	h.mail.failNext(&email.DeliveryError{Status: 503})
	h.buy(s, "standard", 1)
	if got := h.do(http.MethodGet, "/api/v1/me/subscription", nil, h.auth(s.access)); got.str("plan", "code") != "standard" {
		t.Fatalf("subscription: %s", got.raw)
	}
	h.tick()
	if got := h.text(`SELECT status || ':' || error_code FROM notification_deliveries WHERE channel = 'email'`); got != "retrying:HTTP_503" {
		t.Fatalf("email delivery = %s", got)
	}
	if got := h.text(`SELECT status FROM notification_deliveries WHERE channel = 'push'`); got != domain.DeliveryAccepted {
		t.Fatalf("push delivery = %s", got)
	}

	// The notification store is unavailable: the payment still succeeds and the failure is logged.
	if _, err := h.db.Writer().Exec(`ALTER TABLE notifications RENAME TO notifications_unavailable`); err != nil {
		t.Fatal(err)
	}
	other := h.signIn("unlucky-two@example.com")
	paymentID := h.buy(other, "pro", 1)
	if got := h.text(`SELECT status FROM payments WHERE id = ?`, paymentID); got != "succeeded" {
		t.Fatalf("payment = %s", got)
	}
	if got := h.text(`SELECT p.code FROM subscriptions s JOIN plans p ON p.id = s.plan_id
		WHERE s.user_id = ? AND s.status = 'active'`, other.userID); got != "pro" {
		t.Fatalf("plan = %s", got)
	}
	if !strings.Contains(h.logs.String(), "automatic notification failed") {
		t.Fatal("the failure is logged")
	}
}

// ---------------------------------------------------------------- expiring / expired

// Мерзімі бітуге жақын және біткен тариф туралы — әр кезеңге бір-ақ рет.
func TestSubscriptionRemindersAreSentOncePerPeriod(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	s := h.signIn("expiring@example.com")
	h.mustRegister(installation(installID(631), "android", fcmToken(631)), s.access)
	expires := h.pushClock.Now().Add(48 * time.Hour)
	sub := h.subscribe(s.userID, "standard", expires)

	for i := 0; i < 3; i++ {
		h.events.SweepSubscriptions(ctx)
	}
	if n := h.notices(s.userID, domain.TypeSubscriptionExpiring); n != 1 {
		t.Fatalf("reminders = %d", n)
	}
	h.tick()
	msgs := h.pushesOf(domain.TypeSubscriptionExpiring)
	if len(msgs) != 1 || msgs[0].Title != "Жазылым мерзімі аяқталуға жақын" ||
		msgs[0].Body != "«"+h.planName("standard", "kk")+"» тарифі 12.03.2026 дейін жарамды. Жауап лимитін сақтау үшін тарифті ұзартыңыз." ||
		msgs[0].Data["link"] != "aireply://subscription" {
		t.Fatalf("reminder = %+v", msgs)
	}

	// A new end date is a new period: one more reminder for it.
	later := expires.Add(12 * time.Hour)
	if _, err := h.db.Writer().Exec(`UPDATE subscriptions SET expires_at = ? WHERE id = ?`, later.UnixMilli(), sub.ID); err != nil {
		t.Fatal(err)
	}
	h.events.SweepSubscriptions(ctx)
	h.events.SweepSubscriptions(ctx)
	if n := h.notices(s.userID, domain.TypeSubscriptionExpiring); n != 2 {
		t.Fatalf("reminders after the extension = %d", n)
	}

	// After the end: one "ended" notice, whether or not the expiry job ran first.
	h.pushClock.Advance(61 * time.Hour)
	h.events.SweepSubscriptions(ctx)
	if _, err := h.store.ExpireDueSubscriptions(ctx, h.pushClock.Now()); err != nil {
		t.Fatal(err)
	}
	h.events.SweepSubscriptions(ctx)
	if n := h.notices(s.userID, domain.TypeSubscriptionExpired); n != 1 {
		t.Fatalf("expiry notices = %d", n)
	}
	h.tick()
	if msgs := h.pushesOf(domain.TypeSubscriptionExpired); len(msgs) != 1 || msgs[0].Title != "Жазылым мерзімі аяқталды" {
		t.Fatalf("expired = %+v", msgs)
	}

	// More than a day later nothing new is sent.
	h.pushClock.Advance(48 * time.Hour)
	h.events.SweepSubscriptions(ctx)
	if n := h.scalar(`SELECT COUNT(*) FROM notifications WHERE user_id = ?`, s.userID); n != 3 {
		t.Fatalf("notifications = %d", n)
	}
}

// Ақылы тарифі ауыстырылған, бос және әлі ұзақ жарамды тарифтер туралы ескерту жоқ.
func TestSubscriptionRemindersSkipReplacedAndDistantPlans(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	renewed := h.signIn("renewed@example.com")
	old := h.subscribe(renewed.userID, "standard", h.pushClock.Now().Add(-2*time.Hour))
	if _, err := h.db.Writer().Exec(`UPDATE subscriptions SET status = 'expired' WHERE id = ?`, old.ID); err != nil {
		t.Fatal(err)
	}
	// Bought another paid plan before the sweep saw the old one end.
	renewal := h.pushClock.Now().AddDate(0, 0, 30)
	if _, err := h.store.CreateSubscription(ctx, domain.Subscription{
		UserID: renewed.userID, PlanID: h.planID("pro"), Status: domain.SubActive, Source: "payment",
		StartedAt: h.pushClock.Now(), ExpiresAt: &renewal,
	}); err != nil {
		t.Fatal(err)
	}
	distant := h.signIn("distant@example.com")
	h.subscribe(distant.userID, "pro", h.pushClock.Now().AddDate(0, 0, 20))

	h.events.SweepSubscriptions(ctx)
	if n := h.scalar(`SELECT COUNT(*) FROM notifications`); n != 0 {
		t.Fatalf("notifications = %d", n)
	}
}

// ---------------------------------------------------------------- quota

// quota_low шекте бір рет, quota_exhausted нөлде бір рет, келесі күні қайта; лимиттен кейінгі сұраныстар қайталамайды.
func TestQuotaNoticesOncePerDay(t *testing.T) {
	for _, tc := range []struct {
		code             string
		limit, threshold int
	}{{"free", 7, 1}, {"standard", 30, 3}, {"pro", 50, 5}} {
		t.Run(tc.code, func(t *testing.T) {
			h := newHarness(t)
			s := h.signIn(tc.code + "-quota@example.com")
			h.mustRegister(installation(installID(641), "android", fcmToken(641)), s.access)
			if tc.code != "free" {
				h.subscribe(s.userID, tc.code, h.clock.Now().AddDate(0, 0, 30))
			}
			for used := 1; used <= tc.limit; used++ {
				res := h.generate(s.access, "Сәлеметсіз бе! Бағасы қанша?")
				if res.status != http.StatusOK {
					t.Fatalf("generation %d: %d %s", used, res.status, res.raw)
				}
				remaining := tc.limit - used
				if got := int(res.num("usage", "remaining_today")); got != remaining {
					t.Fatalf("generation %d: remaining_today = %d", used, got)
				}
				wantLow, wantExhausted := 0, 0
				if remaining <= tc.threshold {
					wantLow = 1
				}
				if remaining == 0 {
					wantExhausted = 1
				}
				if low, out := h.notices(s.userID, domain.TypeQuotaLow), h.notices(s.userID, domain.TypeQuotaExhausted); low != wantLow || out != wantExhausted {
					t.Fatalf("after %d of %d: quota_low = %d, quota_exhausted = %d", used, tc.limit, low, out)
				}
			}
			for i := 0; i < 3; i++ {
				if res := h.generate(s.access, "Тағы бір сұрақ"); res.status != http.StatusTooManyRequests || res.errorCode() != "DAILY_LIMIT_REACHED" {
					t.Fatalf("past the limit: %d %s", res.status, res.raw)
				}
			}
			if keys := h.noticeKeys(s.userID); strings.Join(keys, ",") != "quota_exhausted:day:2026-03-10,quota_low:day:2026-03-10" {
				t.Fatalf("keys = %v", keys)
			}

			h.tick()
			low := h.pushesOf(domain.TypeQuotaLow)
			wantBody := "Бүгін тағы " + strconv.Itoa(tc.threshold) + " жауап жасай аласыз (күндік лимит — " + strconv.Itoa(tc.limit) + "). Лимит түн ортасында жаңарады."
			if len(low) != 1 || low[0].Title != "Жауаптар таусылуға жақын" || low[0].Body != wantBody ||
				low[0].Data["link"] != "aireply://subscription" || low[0].Data["category"] != domain.CategorySubscription {
				t.Fatalf("quota_low push = %+v", low)
			}
			out := h.pushesOf(domain.TypeQuotaExhausted)
			if len(out) != 1 || out[0].Title != "Жауап лимиті таусылды" || !strings.Contains(out[0].Body, "Бүгінгі "+strconv.Itoa(tc.limit)+" жауаптың") {
				t.Fatalf("quota_exhausted push = %+v", out)
			}

			// The next day (app timezone) is a new period.
			h.clock.Advance(24 * time.Hour)
			s = h.refresh(s)
			for i := 0; i < tc.limit-tc.threshold; i++ {
				if res := h.generate(s.access, "Ертеңгі сұрақ"); res.status != http.StatusOK {
					t.Fatalf("next day %d: %d %s", i, res.status, res.raw)
				}
			}
			if n := h.notices(s.userID, domain.TypeQuotaLow); n != 2 {
				t.Fatalf("quota_low on the second day = %d", n)
			}
			if got := h.text(`SELECT MAX(idempotency_key) FROM notifications WHERE type = 'quota_low'`); got != "quota_low:day:2026-03-11" {
				t.Fatalf("second day key = %q", got)
			}
		})
	}
}

// Айлық лимит: аз қалды, бітті, кейінгі бас тарту қайталамайды; күндік хабар бұл кезде жоқ.
func TestMonthlyQuotaNotices(t *testing.T) {
	h := newHarness(t)
	if _, err := h.db.Writer().Exec(`UPDATE plans SET monthly_message_limit = 5 WHERE code = 'pro'`); err != nil {
		t.Fatal(err)
	}
	s := h.signIn("monthly@example.com")
	h.setPreferredLanguage(s.access, "en")
	h.mustRegister(installation(installID(651), "ios", fcmToken(651)), s.access)
	h.subscribe(s.userID, "pro", h.clock.Now().AddDate(0, 0, 30))
	for i := 1; i <= 5; i++ {
		if res := h.generate(s.access, "Hello"); res.status != http.StatusOK {
			t.Fatalf("generation %d: %d %s", i, res.status, res.raw)
		}
		if i == 3 && h.scalar(`SELECT COUNT(*) FROM notifications`) != 0 {
			t.Fatal("two left of five is above the threshold")
		}
	}
	for i := 0; i < 2; i++ {
		if res := h.generate(s.access, "Hello"); res.status != http.StatusTooManyRequests || res.errorCode() != "MONTHLY_LIMIT_REACHED" {
			t.Fatalf("past the monthly limit: %d %s", res.status, res.raw)
		}
	}
	if keys := h.noticeKeys(s.userID); strings.Join(keys, ",") != "quota_exhausted:month:2026-03,quota_low:month:2026-03" {
		t.Fatalf("keys = %v", keys)
	}
	h.tick()
	if low := h.pushesOf(domain.TypeQuotaLow); len(low) != 1 || low[0].Body != "1 of 5 replies left this month." {
		t.Fatalf("monthly low = %+v", low)
	}
	if out := h.pushesOf(domain.TypeQuotaExhausted); len(out) != 1 || !strings.Contains(out[0].Body, "all 5 replies for this month") {
		t.Fatalf("monthly exhausted = %+v", out)
	}
}

// Лимиті іс жүзінде шексіз тариф, симулятор және ескі install-токен — квота хабары жоқ.
func TestQuotaNoticesSkipUnlimitedPlansAndExcludedAccounts(t *testing.T) {
	h := newHarness(t)
	if _, err := h.db.Writer().Exec(`UPDATE plans SET daily_message_limit = 100000 WHERE code = 'pro'`); err != nil {
		t.Fatal(err)
	}
	s := h.signIn("unlimited@example.com")
	h.subscribe(s.userID, "pro", h.clock.Now().AddDate(0, 0, 30))
	for i := 0; i < 3; i++ {
		if res := h.generate(s.access, "Сәлем"); res.status != http.StatusOK {
			t.Fatalf("generation: %d %s", res.status, res.raw)
		}
	}

	sim := h.signInSimulator()
	headers := sim.headers(h.cfg.Admin.CookieName)
	limit := int(h.do(http.MethodGet, "/api/v1/simulator/account", nil, headers).num("usage", "daily_limit"))
	body := map[string]any{"source_text": "Hello, is it available?", "instruction": "", "language": "en",
		"template_id": "client", "platform": "android"}
	for i := 0; i <= limit; i++ {
		h.do(http.MethodPost, "/api/v1/simulator/generate", body, headers)
	}
	if res := h.do(http.MethodPost, "/api/v1/simulator/generate", body, headers); limit <= 0 || res.errorCode() != "DAILY_LIMIT_REACHED" {
		t.Fatalf("the simulator reached its limit of %d: %d %s", limit, res.status, res.raw)
	}

	legacy := h.do(http.MethodPost, "/v1/auth/register", map[string]any{"install_id": "5C4D3B2A-1111-2222-3333-444455556666"}, nil)
	token := legacy.str("token")
	for i := 0; i < 8; i++ {
		h.do(http.MethodPost, "/v1/reply/generate", map[string]any{"message": "Сәлеметсіз бе!", "template_id": "client"}, h.auth(token))
	}
	if used := h.scalar(`SELECT COALESCE(SUM(used), 0) FROM usage_daily d JOIN users u ON u.id = d.user_id WHERE u.kind = 'legacy_install'`); used != 7 {
		t.Fatalf("the legacy client reached its limit: used = %d", used)
	}

	if n := h.scalar(`SELECT COUNT(*) FROM notifications`); n != 0 {
		t.Fatalf("notifications = %d", n)
	}
}

// ---------------------------------------------------------------- push off

// Push өшірулі: оқиғалар бәрібір жазылады (skipped push_disabled), хат кетеді, қате жоқ.
func TestBusinessEventsAreRecordedWhenPushIsOff(t *testing.T) {
	h := newHarness(t, withEnv("PUSH_NOTIFICATIONS_ENABLED", "false"))
	s := h.signIn("push-off@example.com")
	h.mustRegister(installation(installID(661), "android", fcmToken(661)), s.access)

	h.buy(s, "standard", 1)
	for i := 0; i < 30; i++ {
		if res := h.generate(s.access, "Сәлем"); res.status != http.StatusOK {
			t.Fatalf("generation %d: %d %s", i, res.status, res.raw)
		}
	}
	if _, err := h.db.Writer().Exec(`UPDATE subscriptions SET expires_at = ? WHERE user_id = ? AND status = 'active'`,
		h.pushClock.Now().Add(24*time.Hour).UnixMilli(), s.userID); err != nil {
		t.Fatal(err)
	}
	h.events.SweepSubscriptions(context.Background())
	h.tick()

	types := []string{domain.TypeQuotaExhausted, domain.TypeQuotaLow, domain.TypeSubscriptionActivated, domain.TypeSubscriptionExpiring}
	for _, kind := range types {
		got := h.text(`SELECT d.status || ':' || d.error_code FROM notification_deliveries d
			JOIN notifications n ON n.id = d.notification_id WHERE n.type = ? AND d.channel = 'push'`, kind)
		if got != "skipped:push_disabled" {
			t.Fatalf("%s push row = %q", kind, got)
		}
	}
	var stored []string
	rows, err := h.db.Reader().Query(`SELECT type FROM notifications WHERE user_id = ?`, s.userID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var kind string
		_ = rows.Scan(&kind)
		stored = append(stored, kind)
	}
	_ = rows.Close()
	sort.Strings(stored)
	if strings.Join(stored, ",") != strings.Join(types, ",") {
		t.Fatalf("notifications = %v", stored)
	}
	if len(h.push.messages()) != 0 {
		t.Fatal("nothing is pushed while push is off")
	}
	if mails := h.mail.messages(); len(mails) != 1 || !strings.HasPrefix(mails[0].Content.Subject, domain.TypeSubscriptionActivated) {
		t.Fatalf("the activation e-mail still goes out: %+v", mails)
	}
}

// ---------------------------------------------------------------- texts

var templatePlaceholder = regexp.MustCompile(`\{[a-z_]+\}`)

// Автоматты хабарламалардың мәтіні төрт тілде де бар, орын толтырғыштары ағылшынмен бірдей.
func TestAutomaticNotificationTextsAreCompleteInEveryLanguage(t *testing.T) {
	en := loadLocale(t, "en")
	var keys []string
	for _, kind := range domain.AutomaticNotificationTypes {
		keys = append(keys, "push."+kind+".title", "push."+kind+".body")
	}
	keys = append(keys, "push.quota_low.body_month", "push.quota_exhausted.body_month")
	for _, part := range []string{"subject", "greeting", "body", "expires", "manage", "footer"} {
		keys = append(keys, "email.subscription_activated."+part)
	}
	bundle, err := localization.Load()
	if err != nil {
		t.Fatal(err)
	}
	templates := email.NotificationTemplates{Translate: bundle.T, Brand: "AI Reply"}
	for _, locale := range domain.Locales {
		texts := loadLocale(t, locale)
		for _, key := range keys {
			v, ok := texts[key]
			if !ok || strings.TrimSpace(v) == "" {
				t.Errorf("%s: %s is missing", locale, key)
				continue
			}
			want := templatePlaceholder.FindAllString(en[key], -1)
			got := templatePlaceholder.FindAllString(v, -1)
			sort.Strings(want)
			sort.Strings(got)
			if strings.Join(want, ",") != strings.Join(got, ",") {
				t.Errorf("%s: %s placeholders %v, want %v", locale, key, got, want)
			}
			if strings.HasPrefix(key, "push.") && len([]rune(v)) > 160 {
				t.Errorf("%s: %s is too long for a notification", locale, key)
			}
		}
		content, err := templates.Render(domain.TypeSubscriptionActivated, locale,
			map[string]string{"plan": "Pro", "limit": "50", "date": "09.04.2026"})
		if err != nil || strings.Contains(content.Text+content.Subject, "{") || !strings.Contains(content.Text, "09.04.2026") {
			t.Errorf("%s: e-mail = %+v, %v", locale, content, err)
		}
	}
}
