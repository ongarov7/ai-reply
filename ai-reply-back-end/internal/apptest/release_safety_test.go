package apptest

import (
	"context"
	"html"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/payments"
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/subscriptions"
)

// ---------------------------------------------------------------- landing

// Лендинг осы шығарылымда шын болмайтын нәрсені айтпайды; дүкен сілтемелері берілсе — нақты сілтеме.
func TestLandingClaimsMatchTheRelease(t *testing.T) {
	h := newHarness(t)
	for locale, needles := range map[string][]string{
		"en": {"Your own instructions", "Give Friend, Client, Business and Work your own notes and rules", "platforms: iPhone and Android"},
		"ru": {"Свои указания", "«Друг», «Клиент», «Бизнес» и «Работа»", "платформы: iPhone и Android"},
		"kk": {"Өз нұсқауларыңыз", "«Дос», «Клиент», «Бизнес» және «Жұмыс»", "платформа: iPhone және Android"},
		"uz": {"Oʻz koʻrsatmalaringiz", "«Doʻst», «Mijoz», «Biznes» va «Ish»", "platforma: iPhone va Android"},
	} {
		_, page := fetch(t, h, "/?lang="+locale)
		page = html.UnescapeString(page)
		for _, needle := range needles {
			if !strings.Contains(page, needle) {
				t.Errorf("%s landing is missing %q", locale, needle)
			}
		}
		for _, stale := range []string{"data-count=\"12\"", "Saved templates", "Готовые шаблоны", "Дайын үлгілер", "Tayyor shablonlar",
			"countries supported", "стран поддерживаются", "ел нөмірлері", "mamlakat raqamlari", "phone number", "номер телефона", "телефон нөмірі", "telefon raqami"} {
			if strings.Contains(page, stale) {
				t.Errorf("%s landing still says %q", locale, stale)
			}
		}
		if strings.Count(page, `class="store"`) != 2 || strings.Contains(page, `<a class="store"`) {
			t.Errorf("%s: without store links both badges say coming soon", locale)
		}
	}

	live := newHarness(t, withEnv("APP_STORE_URL", "https://apps.apple.com/kz/app/ai-reply/id0000000000"),
		withEnv("PLAY_STORE_URL", ""))
	_, page := fetch(t, live, "/?lang=en")
	page = html.UnescapeString(page)
	for _, needle := range []string{`<a class="store" href="https://apps.apple.com/kz/app/ai-reply/id0000000000" rel="noopener"><small>Available now</small><b>App Store</b></a>`,
		`<span class="store"><small>Coming soon</small><b>Google Play</b></span>`, "Get AI Reply"} {
		if !strings.Contains(page, needle) {
			t.Errorf("landing with an App Store link is missing %q", needle)
		}
	}
	if strings.Contains(page, "Coming to the App Store and Google Play") {
		t.Error("a live app is still announced as coming")
	}
}

// ---------------------------------------------------------------- payments and plan notices

// Production-да тексерілмеген (демо) төлем ешқашан «тариф қосылды» хабарын жібермейді.
func TestUnverifiedPaymentsAreNeverAnnouncedInProduction(t *testing.T) {
	for _, production := range []bool{true, false} {
		h := newHarness(t)
		s := h.signIn("announce@example.com")
		h.openStore("pro")
		subs := subscriptions.New(h.store, plans.New(h.store), h.cfg.App.Location()).WithClock(h.clock)
		// Config refuses demo checkout in production; the service keeps the rule on its own.
		svc := payments.New(h.store, subs, payments.DemoProvider{}, "demo").WithEvents(h.events).
			WithDemoCheckout(true).WithProduction(production)
		ctx := context.Background()
		intent, err := svc.Start(ctx, s.userID, h.planID("pro"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Confirm(ctx, s.userID, intent.PaymentID); err != nil {
			t.Fatal(err)
		}
		want := 1
		if production {
			want = 0
		}
		if n := h.notices(s.userID, domain.TypeSubscriptionActivated); n != want {
			t.Fatalf("production %v: activation notices = %d, want %d", production, n, want)
		}
		// Reconciliation keeps the same rule: an unannounced demo payment stays silent.
		h.events.WithDemoPayments(!production).ReconcileActivations(ctx)
		if n := h.notices(s.userID, domain.TypeSubscriptionActivated); n != want {
			t.Fatalf("production %v after reconciliation: %d", production, n)
		}
	}
}

// Сол тарифті қайта беру — тек мерзімі ауысады: жаңа жазылым да, екінші хабар да жоқ.
func TestAssigningTheCurrentPlanAgainOnlyMovesItsEnd(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("same-plan@example.com")
	admin := h.signInAdmin()
	h.assignPlan(admin, s.userID, "pro", "2026-04-09")
	first := h.text(`SELECT id FROM subscriptions WHERE user_id = ? AND status = 'active'`, s.userID)

	h.assignPlan(admin, s.userID, "pro", "2026-05-09")
	h.assignPlan(admin, s.userID, "pro", "")
	if n := h.scalar(`SELECT COUNT(*) FROM subscriptions WHERE user_id = ? AND plan_id = ?`, s.userID, h.planID("pro")); n != 1 {
		t.Fatalf("pro subscriptions = %d", n)
	}
	if got := h.text(`SELECT id FROM subscriptions WHERE user_id = ? AND status = 'active'`, s.userID); got != first {
		t.Fatal("the subscription was replaced")
	}
	// Without a date the plan runs one period from now, as for a new assignment.
	ent := h.entitlement(s.userID)
	if ent.Subscription == nil || ent.Subscription.ExpiresAt == nil ||
		!ent.Subscription.ExpiresAt.Equal(h.clock.Now().AddDate(0, 0, ent.Plan.PeriodDays)) {
		t.Fatalf("expiry = %v", ent.Subscription)
	}
	if n := h.notices(s.userID, domain.TypeSubscriptionActivated); n != 1 {
		t.Fatalf("activation notices = %d", n)
	}

	// A different plan is a new subscription with its own notice.
	h.assignPlan(admin, s.userID, "standard", "2026-04-09")
	if n := h.notices(s.userID, domain.TypeSubscriptionActivated); n != 2 {
		t.Fatalf("activation notices after a plan change = %d", n)
	}
}

// Сверка: жазылмай қалған «тариф қосылды» бір рет жіберіледі; тегін, біткен, симулятор — жоқ.
func TestActivationReconciliationCatchesMissedNotices(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	s := h.signIn("missed@example.com")
	h.mustRegister(installation(installID(701), "android", fcmToken(701)), s.access)

	// An admin plan whose notice was lost (written straight to the database).
	sub := h.subscribe(s.userID, "pro", h.clock.Now().AddDate(0, 0, 30))
	// A payment confirmed while notifications were down.
	payer := h.signIn("missed-payer@example.com")
	h.openStore("standard")
	quiet := payments.New(h.store, subscriptions.New(h.store, plans.New(h.store), h.cfg.App.Location()).WithClock(h.clock),
		payments.DemoProvider{}, "demo").WithDemoCheckout(true)
	intent, err := quiet.Start(ctx, payer.userID, h.planID("standard"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := quiet.Confirm(ctx, payer.userID, intent.PaymentID); err != nil {
		t.Fatal(err)
	}
	// Nothing to say about these.
	ended := h.signIn("missed-ended@example.com")
	h.subscribe(ended.userID, "pro", h.clock.Now().Add(-time.Hour))
	free := h.signIn("missed-free@example.com")
	h.subscribe(free.userID, "free", h.clock.Now().AddDate(0, 0, 30))

	for i := 0; i < 3; i++ {
		h.events.ReconcileActivations(ctx)
	}
	if keys := h.noticeKeys(s.userID); len(keys) != 1 || keys[0] != "subscription_activated:sub:"+sub.ID {
		t.Fatalf("admin plan keys = %v", keys)
	}
	if keys := h.noticeKeys(payer.userID); len(keys) != 1 || keys[0] != "subscription_activated:payment:"+intent.PaymentID {
		t.Fatalf("payment keys = %v", keys)
	}
	for _, quietUser := range []string{ended.userID, free.userID} {
		if keys := h.noticeKeys(quietUser); len(keys) != 0 {
			t.Fatalf("unexpected notices %v", keys)
		}
	}
	// The event itself arriving late adds nothing: the key is the same.
	h.events.PlanAssigned(ctx, sub)
	if n := h.notices(s.userID, domain.TypeSubscriptionActivated); n != 1 {
		t.Fatalf("notices after the late event = %d", n)
	}
	h.tick()
	if msgs := h.pushesOf(domain.TypeSubscriptionActivated); len(msgs) != 1 {
		t.Fatalf("pushes = %d", len(msgs))
	}
}

// ---------------------------------------------------------------- polish cap

// Күндік polish шегі толса — қате емес, «ұсыныс жоқ»; провайдер шақырылмайды; келесі күні қайта.
func TestPolishDailyCapAnswersNoSuggestion(t *testing.T) {
	h := newHarness(t, withEnv("LIMIT_POLISH_PER_DAY", "2"))
	s := h.signIn("polish-cap@example.com")
	h.provider.reply = "Скажи ей, что сегодня не получится, давай завтра вечером."

	for i := 0; i < 2; i++ {
		if res := h.polish(s.access, map[string]any{"text": polishNote}); res.status != http.StatusOK || res.body["changed"] != true {
			t.Fatalf("polish %d: %d %s", i+1, res.status, res.raw)
		}
	}
	calls := h.provider.calls
	capped := h.polish(s.access, map[string]any{"text": " " + polishNote + " "})
	if capped.status != http.StatusOK || capped.body["changed"] != false || capped.str("text") != polishNote {
		t.Fatalf("capped polish: %d %s", capped.status, capped.raw)
	}
	if h.provider.calls != calls {
		t.Fatal("the provider was called past the cap")
	}
	// Replies are not affected by the polish cap.
	if res := h.generate(s.access, "Сәлеметсіз бе!"); res.status != http.StatusOK {
		t.Fatalf("reply after the polish cap: %d %s", res.status, res.raw)
	}
	// Another person has their own allowance.
	other := h.signIn("polish-cap-other@example.com")
	if res := h.polish(other.access, map[string]any{"text": polishNote}); res.body["changed"] != true {
		t.Fatalf("another user is capped: %s", res.raw)
	}

	h.clock.Advance(24 * time.Hour)
	s = h.refresh(s)
	if res := h.polish(s.access, map[string]any{"text": polishNote}); res.body["changed"] != true {
		t.Fatalf("the next server day: %s", res.raw)
	}
}

// ---------------------------------------------------------------- campaigns

// Әкімші науқаны «қауіпсіздік» не «тіркелгі» санатын пайдалана алмайды; форма оларды ұсынбайды.
func TestCampaignsCannotUseSecurityOrAccountCategories(t *testing.T) {
	h := newHarness(t)
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)

	meta := h.do(http.MethodGet, "/api/v1/admin/notifications", nil, headers)
	categories, _ := meta.body["categories"].([]any)
	if len(categories) != 3 || categories[0] != "subscription" || categories[1] != "system" || categories[2] != "marketing" {
		t.Fatalf("form categories = %v", categories)
	}
	for i, category := range []string{domain.CategorySecurity, domain.CategoryAccount} {
		res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{"category": category}),
			withKey(headers, "campaign-category-"+category+"-"+string(rune('a'+i))))
		if res.status != http.StatusBadRequest || res.str("error", "details", "field") != "category" {
			t.Fatalf("%s campaign: %d %s", category, res.status, res.raw)
		}
		preview := h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview",
			map[string]any{"audience": map[string]any{}, "category": category}, headers)
		if preview.status != http.StatusBadRequest {
			t.Fatalf("%s preview: %d %s", category, preview.status, preview.raw)
		}
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notification_campaigns`); n != 0 {
		t.Fatalf("campaigns = %d", n)
	}
	if res := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{"category": "system"}),
		withKey(headers, "campaign-category-system-1")); res.status != http.StatusCreated {
		t.Fatalf("system campaign: %d %s", res.status, res.raw)
	}
}
