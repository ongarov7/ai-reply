package apptest

import (
	"html"
	"net/http"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/localization"
)

// Сатып алу жоқ кезде лендинг тариф ауыстыруды уәде етпейді; тегін лимит дерекқордан алынады.
func TestLandingPricingWithoutPurchases(t *testing.T) {
	h := newHarness(t)
	_, page := fetch(t, h, "/?lang=en")
	page = html.UnescapeString(page)
	for _, needle := range []string{
		"The free plan gives 7 replies a day.", "AI Reply is free. The free plan has a daily reply limit",
		`href="#download">Get the app</a>`, `data-count="7"`,
	} {
		if !strings.Contains(page, needle) {
			t.Errorf("landing is missing %q", needle)
		}
	}
	for _, promise := range []string{"Choose in the app", "change the plan any time", "{limit}"} {
		if strings.Contains(page, promise) {
			t.Errorf("landing still says %q", promise)
		}
	}

	// The note follows the admin's free limit, in every language.
	admin := h.signInAdmin()
	if res := h.savePlan(admin, h.adminPlan(admin, "free"), map[string]any{"daily_message_limit": 12}); res.status != http.StatusOK {
		t.Fatalf("save free plan: %d %s", res.status, res.raw)
	}
	for locale, note := range map[string]string{
		"en": "The free plan gives 12 replies a day.", "ru": "На бесплатном тарифе 12 ответов в день.",
		"kk": "Тегін тарифте күніне 12 жауап.", "uz": "Bepul tarifda kuniga 12 ta javob.",
	} {
		if _, body := fetch(t, h, "/?lang="+locale); !strings.Contains(html.UnescapeString(body), note) {
			t.Errorf("%s landing has no %q", locale, note)
		}
	}

	// With a plan actually on sale the original copy comes back.
	h.openStore("standard")
	_, page = fetch(t, h, "/?lang=en")
	if page = html.UnescapeString(page); !strings.Contains(page, "Choose in the app") ||
		!strings.Contains(page, "change the plan any time") {
		t.Fatal("landing with a purchasable plan lost its pricing copy")
	}
}

// Автоматты хабарламалар тариф таңдауға не ұзартуға шақырмайды (төрт тілде де).
func TestNotificationTextsDoNotUpsell(t *testing.T) {
	bundle, err := localization.Load()
	if err != nil {
		t.Fatal(err)
	}
	upsell := map[string][]string{
		"en": {"choose a plan", "renew it", "higher limit"},
		"ru": {"выберите тариф", "продлите", "более высоким лимитом"},
		"kk": {"тариф таңдаңыз", "ұзартыңыз", "лимиті жоғары"},
		"uz": {"tarifni tanlang", "tarif tanlang", "uzaytiring", "yuqoriroq"},
	}
	for _, locale := range domain.Locales {
		for _, key := range []string{
			"push.subscription_expiring.body", "push.subscription_expired.body",
			"push.quota_exhausted.body", "push.quota_exhausted.body_month",
			"push.quota_low.body", "push.quota_low.body_month",
		} {
			text := strings.ToLower(bundle.T(locale, key))
			for _, phrase := range upsell[locale] {
				if strings.Contains(text, phrase) {
					t.Errorf("%s %s still says %q: %s", locale, key, phrase, text)
				}
			}
		}
	}
}

// Құпиялық саясаты: OpenAI, нақты сақтау мерзімдері, жою беті, шағымдар; оператор тек берілсе.
func TestPrivacyPolicyDescribesTheRealProcessing(t *testing.T) {
	h := newHarness(t, withEnv("CONTACT_EMAIL", "support@ai-reply.kz"), withEnv("RETENTION_PRODUCT_EVENTS_DAYS", "200"))
	_, page := fetch(t, h, "/privacy?lang=en")
	page = html.UnescapeString(page)
	for _, needle := range []string{
		"OpenAI (USA)", "store=false", "Smart correction", "Resend", "Firebase Cloud Messaging",
		"app events — after 200 days", "request metadata — after 400 days",
		"sign-in and deletion codes (from when they are issued) — after 30 days",
		"(from when it was last seen) — after 180 days", "finished notifications and deliveries — after 180 days",
		"http://localhost:8084/account/delete", "Send the reply text with the report", "Settings → Privacy",
		"support@ai-reply.kz", "not intended for children under 13", "advertising identifiers",
	} {
		if !strings.Contains(page, needle) {
			t.Errorf("privacy policy is missing %q", needle)
		}
	}
	if strings.Contains(page, "operated by") || strings.Contains(page, "Operator details") {
		t.Error("an operator is named although LEGAL_OPERATOR_NAME is empty")
	}
	for locale, needle := range map[string]string{
		"ru": "через 200 дней", "kk": "200 күннен кейін", "uz": "200 kundan keyin",
	} {
		if _, body := fetch(t, h, "/privacy?lang="+locale); !strings.Contains(html.UnescapeString(body), needle) ||
			!strings.Contains(body, "OpenAI") || !strings.Contains(body, "store=false") {
			t.Errorf("%s privacy policy does not state the retention or the AI provider", locale)
		}
	}

	named := newHarness(t, withEnv("LEGAL_OPERATOR_NAME", "Operator Name"), withEnv("LEGAL_OPERATOR_DETAILS", "Address, registration 123"),
		withEnv("CONTACT_EMAIL", ""))
	_, page = fetch(t, named, "/privacy?lang=en")
	page = html.UnescapeString(page)
	for _, needle := range []string{"The service is operated by Operator Name.", "Operator details: Address, registration 123.",
		"the support page http://localhost:8084/support"} {
		if !strings.Contains(page, needle) {
			t.Errorf("privacy policy with an operator is missing %q", needle)
		}
	}
	if _, offer := fetch(t, named, "/offer?lang=ru"); !strings.Contains(html.UnescapeString(offer), "Оферту предлагает Operator Name.") {
		t.Error("the offer does not name the configured operator")
	}
}

// Оферта: қазіргі кіру тәсілдері, тегін қызмет, ақылы тариф тек дүкендер арқылы.
func TestTermsDescribeTheCurrentService(t *testing.T) {
	h := newHarness(t)
	for locale, needles := range map[string][]string{
		"en": {"Apple, Google or a one-time code", "currently free", "App Store or Google Play", "9. Contact"},
		"ru": {"через Apple, Google или по одноразовому коду", "Сейчас сервис бесплатный", "App Store или Google Play", "9. Контакты"},
		"kk": {"Apple, Google арқылы", "Қазір қызмет тегін", "App Store не Google Play", "9. Байланыс"},
		"uz": {"Apple, Google orqali", "Hozir xizmat bepul", "App Store yoki Google Play", "9. Aloqa"},
	} {
		_, page := fetch(t, h, "/offer?lang="+locale)
		page = html.UnescapeString(page)
		for _, needle := range needles {
			if !strings.Contains(page, needle) {
				t.Errorf("%s offer is missing %q", locale, needle)
			}
		}
		for _, stale := range []string{"phone number", "номеру телефона", "телефон нөмірі", "telefon raqami"} {
			if strings.Contains(page, stale) {
				t.Errorf("%s offer still offers phone sign-in", locale)
			}
		}
	}
}
