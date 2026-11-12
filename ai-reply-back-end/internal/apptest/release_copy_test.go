package apptest

import (
	"context"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/transport/web"
)

// Сатып алу жоқ кезде лендинг тариф ауыстыруды уәде етпейді; тегін лимит дерекқордан алынады.
func TestLandingPricingWithoutPurchases(t *testing.T) {
	h := newHarness(t)
	_, page := fetch(t, h, "/?lang=en")
	page = html.UnescapeString(page)
	for _, needle := range []string{
		"The free plan gives 7 replies a day.", "Start free. The app and the keyboard show how many replies you have left.",
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

func releaseCopyPages(t *testing.T, h *harness, bundle *localization.Bundle, prices *catalog.Service) http.Handler {
	t.Helper()
	s, err := web.New(web.Deps{Config: h.cfg, Plans: plans.New(h.store), Catalog: prices,
		Bundle: bundle, Limiter: middleware.NewLimiter(), Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	return mux
}

func releaseCopyPage(t *testing.T, handler http.Handler, path string) string {
	t.Helper()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("%s: HTTP %d", path, w.Code)
	}
	return html.UnescapeString(w.Body.String())
}

// Current website packs are one-time purchases; legacy Base balances stay described in help.
func TestPublicPagesDescribeOneTimeWebsitePacks(t *testing.T) {
	h := newHarness(t, withEnv("TEST_APPROVED_ENTITLEMENTS", "true"))
	bundle, err := localization.Load()
	if err != nil {
		t.Fatal(err)
	}
	prices := catalog.New(h.db)
	if err := prices.EnsureInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	pages := releaseCopyPages(t, h, bundle, prices)
	for locale, want := range map[string]struct {
		bundle, support, website string
		stale                    []string
	}{
		"en": {"500 replies", "Purchased reply packs never expire.", "Website prices are in tenge",
			[]string{"1,000 replies each paid month", "Every night", "renews every night", "resets at midnight", "left today", "the next day", "daily reply limit"}},
		"ru": {"500 ответов", "Купленные пакеты ответов не сгорают.", "Цены сайта указаны в тенге",
			[]string{"1 000 ответов каждый оплаченный месяц", "Каждую ночь", "каждую ночь", "в полночь", "осталось сегодня", "на следующий день", "дневной лимит"}},
		"kk": {"500 жауап", "Сатып алынған жауап пакеттерінің мерзімі шектелмейді.", "Сайттағы бағалар теңгемен көрсетілген",
			[]string{"Әр төленген айға 1 000 жауап", "Күн сайын түн ортасында", "әр түні", "түн ортасында", "Бүгін неше", "келесі күні", "күндік"}},
		"uz": {"500 javob", "Sotib olingan javob paketlari muddatsiz.", "Saytdagi narxlar tengeda",
			[]string{"Har to‘langan oy uchun 1 000 javob", "Har kuni yarim tunda", "har kecha", "yarim tunda", "Bugun nechta", "ertasi kuni", "kunlik"}},
	} {
		landing := releaseCopyPage(t, pages, "/?lang="+locale)
		support := releaseCopyPage(t, pages, "/support?lang="+locale)
		note := bundle.T(locale, "pricing.catalog_note")
		for _, text := range []string{want.bundle, bundle.T(locale, "pricing.pack_description"), bundle.T(locale, "admin.billing.once"),
			"990 KZT", "130 KZT", "3 990 KZT", "39 990 KZT"} {
			if !strings.Contains(landing, text) {
				t.Errorf("%s landing is missing %q", locale, text)
			}
		}
		if strings.Contains(landing, `<div class="plan-name">Base`) {
			t.Errorf("%s landing still offers a Base subscription", locale)
		}
		if !strings.Contains(support, want.support) || !strings.Contains(support, "Pro") {
			t.Errorf("%s support does not describe permanent packs below Pro", locale)
		}
		if !strings.Contains(note, want.website) || !strings.Contains(note, "iOS") || !strings.Contains(note, "App Store") {
			t.Errorf("%s pricing note does not distinguish website prices from the Apple storefront", locale)
		}
		for _, stale := range want.stale {
			if strings.Contains(landing, stale) || strings.Contains(support, stale) || strings.Contains(note, stale) {
				t.Errorf("%s public pages still say %q", locale, stale)
			}
		}
	}
}

func TestLandingHidesLegacyBaseWithoutPublishedCatalog(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing catalog", true: "invalid catalog"}[broken], func(t *testing.T) {
			h := newHarness(t, withEnv("TEST_APPROVED_ENTITLEMENTS", "true"))
			bundle, err := localization.Load()
			if err != nil {
				t.Fatal(err)
			}
			var prices *catalog.Service
			if broken {
				prices = catalog.New(h.db)
				if err := prices.EnsureInitial(context.Background()); err != nil {
					t.Fatal(err)
				}
				if _, err := h.db.Writer().Exec(`UPDATE billing_catalog_versions SET products_json='{}' WHERE state='published'`); err != nil {
					t.Fatal(err)
				}
			}
			pages := releaseCopyPages(t, h, bundle, prices)
			for _, locale := range domain.Locales {
				page := releaseCopyPage(t, pages, "/?lang="+locale)
				if strings.Contains(page, `<div class="plan-name">Base`) || strings.Contains(page, "1 000") || strings.Contains(page, "1,000") {
					t.Errorf("%s landing advertises legacy Base without a usable catalog", locale)
				}
			}
			// Hiding a public card must not change the existing native subscription.
			if got := h.scalar(`SELECT monthly_message_limit FROM plans WHERE code='base_monthly'`); got != 1000 {
				t.Fatalf("legacy Base entitlement changed: %d", got)
			}
		})
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

// Offer describes the welcome grant, store purchases and one-time bank packs.
func TestTermsDescribeTheCurrentService(t *testing.T) {
	h := newHarness(t)
	for locale, needles := range map[string][]string{
		"en": {"Apple, Google or a one-time code", "20 free replies", "App Store or Google Play", "one-time reply pack", "do not expire", "final confirmation", "9. Contact"},
		"ru": {"через Apple, Google или по одноразовому коду", "20 бесплатных ответов", "App Store или Google Play", "разовый пакет", "не имеют срока действия", "окончательного подтверждения", "9. Контакты"},
		"kk": {"Apple, Google арқылы", "20 тегін жауап", "App Store не Google Play", "бір реттік жауап пакетін", "мерзімі шектелмейді", "түпкілікті растауын", "9. Байланыс"},
		"uz": {"Apple, Google orqali", "20 ta bepul javob", "App Store yoki Google Play", "bir martalik javoblar", "muddati cheklanmagan", "yakuniy tasdigʻini", "9. Aloqa"},
	} {
		_, page := fetch(t, h, "/offer?lang="+locale)
		page = html.UnescapeString(page)
		for _, needle := range needles {
			if !strings.Contains(page, needle) {
				t.Errorf("%s offer is missing %q", locale, needle)
			}
		}
		for _, stale := range []string{"phone number", "номеру телефона", "телефон нөмірі", "telefon raqami", "currently free", "Сейчас сервис бесплатный", "Қазір қызмет тегін", "Hozir xizmat bepul"} {
			if strings.Contains(page, stale) {
				t.Errorf("%s offer still offers phone sign-in", locale)
			}
		}
	}
}

// Саясат пернетақтаның шын жұмысын айтады: көшірілген не белгіленген мәтін, «Жазу» тек нұсқаумен,
// iPhone-дағы дауыс, Google аты, аудит журналы; Apple-ді «ажыратамыз» тек кілт бапталса.
func TestPrivacyPolicyMatchesWhatTheAppsSend(t *testing.T) {
	h := newHarness(t, withEnv("RETENTION_NOTIFICATIONS_DAYS", "7"))
	_, page := fetch(t, h, "/privacy?lang=en")
	page = html.UnescapeString(page)
	for _, needle := range []string{
		"the message you copied or the text you selected in the current field",
		"When you tap Write, only your instruction and your grammatical gender are sent",
		"Reply to copied", "Nothing else you type in other apps is sent",
		"In the iPhone app you can dictate", "Apple speech recognition", "AI Reply receives only the recognised text",
		"On Apple Watch you can say your instruction", "our server to OpenAI (USA) to be turned into text",
		"The microphone on the AI Reply keyboard works differently", "counts as tapping Reply in the keyboard",
		"turns voice recordings from the Android keyboard and Apple Watch into text",
		"When you open a persona on the watch", "the watch keeps it only while that screen is open",
		"if you enter it or Apple or Google shares it",
		"A log of administrator actions on an account", "is not removed automatically, including after the account is deleted",
		"finished notification deliveries — after 7 days", "the notifications themselves — after 40 days",
		"If you signed in with Apple, you can remove AI Reply in your Apple ID settings",
		"payment and refund status", "BCC on the website", "The card number and CVC are entered on the bank",
		"The renewal consent version and end date are stored",
	} {
		if !strings.Contains(page, needle) {
			t.Errorf("en privacy policy is missing %q", needle)
		}
	}
	for _, stale := range []string{"only the text of AI Reply's own instruction field", "When you tap Reply or Write",
		"Everything else is removed", "disconnect Sign in with Apple", "only if paid plans are ever sold",
		"On Android you can dictate the instruction. Speech is recognised"} {
		if strings.Contains(page, stale) {
			t.Errorf("en privacy policy still says %q", stale)
		}
	}
	for locale, needles := range map[string][]string{
		"ru": {"На Apple Watch инструкцию можно сказать голосом", "Микрофон на клавиатуре AI Reply работает иначе", "Когда вы открываете персону на часах", "то же, что нажатие «Ответить» в клавиатуре", "выделили в текущем поле", "«Ответить на скопированное»", "В приложении для iPhone можно надиктовать",
			"передаст Apple или Google", "Журнал действий администраторов", "сами уведомления — через 40 дней"},
		"kk": {"Apple Watch-та нұсқауды дауыспен айтуға болады", "AI Reply пернетақтасындағы микрофон басқаша жұмыс істейді", "Сағатта персонаны ашқанда", "«Жауап беру» батырмасын басқанмен бірдей", "өзіңіз белгілеген мәтін", "«Көшірілгенге жауап беру»", "iPhone қосымшасында", "Apple немесе Google берсе",
			"Параметрлер → Тіркелгі → Тіркелгіні жою", "хабарламалардың өзі — 40 күннен кейін"},
		"uz": {"Apple Watch’da koʻrsatmani ovoz bilan aytish mumkin", "AI Reply klaviaturasidagi mikrofon boshqacha ishlaydi", "Soatda personani ochganingizda", "«Javob berish»ni bosish bilan bir xil", "oʻzingiz belgilagan matn", "«Nusxalanganga javob berish»", "iPhone ilovasida", "Apple yoki Google bersa",
			"Administratorlarning hisob bilan bogʻliq harakatlari jurnali", "bildirishnomalarning oʻzi — 40 kundan keyin"},
	} {
		_, body := fetch(t, h, "/privacy?lang="+locale)
		body = html.UnescapeString(body)
		for _, needle := range needles {
			if !strings.Contains(body, needle) {
				t.Errorf("%s privacy policy is missing %q", locale, needle)
			}
		}
	}

	// Without the revocation key the deletion page promises nothing about Apple.
	_, deletion := fetch(t, h, "/account/delete?lang=en")
	deletion = html.UnescapeString(deletion)
	if strings.Contains(deletion, "disconnect Sign in with Apple") ||
		!strings.Contains(deletion, "you can also remove AI Reply in your Apple ID settings under Sign in with Apple") {
		t.Errorf("deletion page without the Apple key: %s", deletion)
	}
	if _, kk := fetch(t, h, "/account/delete?lang=kk"); !strings.Contains(kk, "Параметрлер → Тіркелгі → Тіркелгіні жою") {
		t.Error("kk deletion page names a button the apps do not have")
	}

	revoking := newHarness(t, withEnv("APPLE_CLIENT_ID", testAppleAudience), withEnv("APPLE_TEAM_ID", "TEAM123456"),
		withEnv("APPLE_KEY_ID", "KEY1234567"), withEnv("APPLE_PRIVATE_KEY", `-----BEGIN PRIVATE KEY-----\nc2VjcmV0\n-----END PRIVATE KEY-----`))
	for path, needle := range map[string]string{
		"/account/delete?lang=en": "so that we can also disconnect Sign in with Apple",
		"/privacy?lang=en":        "deleting the account in the iPhone app also asks Apple to disconnect Sign in with Apple",
		"/account/delete?lang=ru": "чтобы мы могли отключить и вход с Apple",
	} {
		if _, body := fetch(t, revoking, path); !strings.Contains(html.UnescapeString(body), needle) {
			t.Errorf("%s with the Apple key is missing %q", path, needle)
		}
	}
}

// Лендинг: пернетақта көшірілген не белгіленген мәтінді көреді, iPhone-да толық қолжетімділік керек.
func TestLandingSaysWhatTheKeyboardReads(t *testing.T) {
	h := newHarness(t)
	for locale, needles := range map[string][]string{
		"en": {"copied or selected", "On iPhone it needs Full Access"},
		"ru": {"скопированный или выделенный текст", "нужен полный доступ"},
		"kk": {"көшірілген не белгіленген мәтінді", "толық қолжетімділік керек"},
		"uz": {"nusxalangan yoki belgilangan matnni", "toʻliq ruxsat kerak"},
	} {
		_, page := fetch(t, h, "/?lang="+locale)
		page = html.UnescapeString(page)
		for _, needle := range needles {
			if !strings.Contains(page, needle) {
				t.Errorf("%s landing is missing %q", locale, needle)
			}
		}
		for _, stale := range []string{"Nothing else is accessed", "никаких дополнительных доступов", "қосымша ешқандай рұқсат", "qoʻshimcha ruxsat soʻralmaydi"} {
			if strings.Contains(page, stale) {
				t.Errorf("%s landing still says %q", locale, stale)
			}
		}
	}
}
