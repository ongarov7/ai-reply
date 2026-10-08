package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/legal"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

func clientIP(r *http.Request, trustProxy bool) string { return httpx.ClientIP(r, trustProxy) }

type planView struct {
	Name        string
	Description string
	PriceText   string
	DailyLimit  int
	IsFree      bool
	Featured    bool
}

type landingView struct {
	baseView
	Plans []planView
	Boot  template.JS
}

// demoScene — лендингтегі анимацияланған мысал.
type demoScene struct {
	Incoming         string `json:"incoming"`
	Instruction      string `json:"instruction"`
	Reply            string `json:"reply"`
	LabelIncoming    string `json:"labelIncoming"`
	LabelInstruction string `json:"labelInstruction"`
	LabelReply       string `json:"labelReply"`
}

// demoScenes — үш сценарий: сатылым, кездесу, тапсырыс.
func (s *Server) demoScenes(locale string) []demoScene {
	prefixes := []string{"hero.demo", "hero.demo2", "hero.demo3"}
	scenes := make([]demoScene, 0, len(prefixes))
	for _, prefix := range prefixes {
		scenes = append(scenes, demoScene{
			Incoming:         s.bundle.T(locale, prefix+"_incoming"),
			Instruction:      s.bundle.T(locale, prefix+"_instruction"),
			Reply:            s.bundle.T(locale, prefix+"_reply"),
			LabelIncoming:    s.bundle.T(locale, "hero.demo_label_incoming"),
			LabelInstruction: s.bundle.T(locale, "hero.demo_label_instruction"),
			LabelReply:       s.bundle.T(locale, "hero.demo_label_reply"),
		})
	}
	return scenes
}

// handleLanding — басты бет.
func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	locale := s.locale(w, r)
	list, err := s.plans.Listed(r.Context())
	if err != nil {
		s.log.Error("landing plans failed", "error", err.Error())
	}

	boot, err := json.Marshal(map[string]any{"scenes": s.demoScenes(locale), "locale": locale})
	if err != nil {
		boot = []byte("{}")
	}
	view := landingView{baseView: s.base(locale), Boot: template.JS(boot)}
	for i, p := range list {
		view.Plans = append(view.Plans, planView{
			Name:        p.LocalizedName(locale),
			Description: p.LocalizedDescription(locale),
			PriceText:   traits.FormatMoney(p.Price, p.Currency),
			DailyLimit:  p.DailyLimit,
			IsFree:      p.IsFree,
			Featured:    i == 1,
		})
	}
	s.render(w, "landing", "public_layout", view)
}

type legalView struct {
	baseView
	Title   string
	Updated string
	Body    template.HTML
	Boot    template.JS
}

// handleTerms — пайдалану шарттары.
func (s *Server) handleTerms(w http.ResponseWriter, r *http.Request) {
	locale := s.locale(w, r)
	s.render(w, "legal", "public_layout", legalView{
		baseView: s.base(locale),
		Title:    s.bundle.T(locale, "legal.terms.title"),
		Updated:  legal.UpdatedDate,
		Body:     template.HTML(termsBody(locale, s.cfg.App.ContactEmail())),
		Boot:     template.JS("{}"),
	})
}

// handlePrivacy — құпиялық саясаты.
func (s *Server) handlePrivacy(w http.ResponseWriter, r *http.Request) {
	locale := s.locale(w, r)
	s.render(w, "legal", "public_layout", legalView{
		baseView: s.base(locale),
		Title:    s.bundle.T(locale, "legal.privacy.title"),
		Updated:  legal.UpdatedDate,
		Body:     template.HTML(privacyBody(locale)),
		Boot:     template.JS("{}"),
	})
}

// locale — тілді анықтап, cookie-ге жазады.
func (s *Server) locale(w http.ResponseWriter, r *http.Request) string {
	locale := localization.Detect(r, "en")
	if r.URL.Query().Get("lang") != "" {
		localization.SetCookie(w, locale, s.cfg.App.IsProduction())
	}
	return domain.NormalizeLocale(locale)
}

func termsBody(locale, contactEmail string) string {
	sections := map[string][][2]string{
		"kk": {
			{"1. Жалпы ережелер", "Бұл құжат AI Reply цифрлық қызметін пайдалану туралы жария оферта болып табылады. «Қабылдаймын» түймесін басу, тіркелу немесе қызметті пайдалану осы офертаның толық акцепті болып саналады."},
			{"2. Қызмет", "AI Reply — мессенджерлерге арналған жауап жобаларын жасауға көмектесетін мобильді қосымша мен пернетақта. Қызмет нәтижені автоматты түрде жасайды және мәтінді алушыға өзі жібермейді."},
			{"3. Есептік жазба", "Кіру телефон нөмірі немесе электрондық пошта арқылы орындалады. Пайдаланушы өз құрылғысы мен есептік жазбасына қолжетімділіктің қауіпсіздігіне жауап береді."},
			{"4. Тарифтер мен төлем", "Қолжетімді тариф, баға, мерзім және лимит сатып алу алдында көрсетіледі. Төлем мен қайтару қолданылған дүкеннің немесе төлем провайдерінің ережелеріне және міндетті заң талаптарына сәйкес жүргізіледі."},
			{"5. Пайдалану шарттары", "Қызметті заңсыз, зиянды, алдамшы контент жасауға немесе басқа тұлғалардың құқықтарын бұзуға пайдалануға болмайды. Елеулі бұзушылық кезінде қолжетімділік шектелуі мүмкін."},
			{"6. Жауапкершілік", "Жасалған мәтін — тек жоба. Пайдаланушы оны жіберер алдында тексереді және жіберілген мазмұнға өзі жауап береді. Қызмет үздіксіз немесе қатесіз жұмыс істейтініне кепілдік берілмейді."},
			{"7. Деректер мен зияткерлік құқықтар", "Дербес деректер бөлек Құпиялық саясатына сәйкес өңделеді. Қосымшаға, дизайнға және бағдарламалық кодқа құқықтар AI Reply құқық иесіне тиесілі; пайдаланушыға жеке пайдалану үшін шектеулі құқық беріледі."},
			{"8. Өзгерту және тоқтату", "Офертаның жаңа редакциясы осы бетте жарияланады. Қызметті пайдалануды тоқтату үшін пайдаланушы жазылымнан бас тартып, есептік жазбаны жою туралы сұраныс бере алады."},
			{"9. Байланыс", "Оферта, төлем немесе есептік жазба бойынша сұрақтар: " + contactEmail + "."},
		},
		"ru": {
			{"1. Общие положения", "Настоящий документ является публичной офертой на использование цифрового сервиса AI Reply. Нажатие кнопки согласия, регистрация или использование сервиса означают полный и безоговорочный акцепт оферты."},
			{"2. Предмет оферты", "AI Reply предоставляет мобильное приложение и клавиатуру для подготовки черновиков ответов в мессенджерах. Сервис генерирует текст автоматически и не отправляет его получателю без действия пользователя."},
			{"3. Аккаунт", "Вход выполняется по номеру телефона или электронной почте. Пользователь отвечает за сохранность доступа к своему устройству и аккаунту."},
			{"4. Тарифы, оплата и возврат", "Доступный тариф, цена, срок и лимиты показываются до покупки. Оплата и возврат выполняются по правилам магазина приложений или платёжного провайдера с учётом обязательных требований закона."},
			{"5. Правила использования", "Запрещено использовать сервис для незаконного, вредоносного или вводящего в заблуждение контента и нарушения прав третьих лиц. При существенном нарушении доступ может быть ограничен."},
			{"6. Ответственность", "Сгенерированный текст является черновиком. Пользователь проверяет его до отправки и самостоятельно отвечает за отправленное содержание. Бесперебойная и безошибочная работа сервиса не гарантируется."},
			{"7. Данные и интеллектуальные права", "Персональные данные обрабатываются по отдельной Политике конфиденциальности. Права на приложение, дизайн и программный код принадлежат правообладателю AI Reply; пользователю предоставляется ограниченное право личного использования."},
			{"8. Изменение и прекращение", "Новая редакция оферты публикуется на этой странице. Пользователь может прекратить использование, отменить подписку и направить запрос на удаление аккаунта."},
			{"9. Контакты", "Вопросы по оферте, оплате или аккаунту можно направить на " + contactEmail + "."},
		},
		"en": {
			{"1. General", "This document is a public offer governing use of the AI Reply digital service. Selecting the consent control, registering, or using the service constitutes full acceptance of this offer."},
			{"2. Service", "AI Reply provides a mobile app and keyboard that prepare draft replies for messengers. The service generates text automatically and does not send it to a recipient without a user action."},
			{"3. Account", "You sign in with a phone number or email address. You are responsible for keeping access to your device and account secure."},
			{"4. Plans, payment and refunds", "The available plan, price, term, and limits are shown before purchase. Payments and refunds follow the applicable app store or payment provider rules and mandatory law."},
			{"5. Acceptable use", "The service must not be used for illegal, harmful, or deceptive content or to violate third-party rights. Access may be restricted for a material violation."},
			{"6. Responsibility", "Generated text is a draft. You review it before sending and remain responsible for sent content. Uninterrupted or error-free operation is not guaranteed."},
			{"7. Data and intellectual property", "Personal data is handled under the separate Privacy Policy. Rights in the app, design, and software belong to the AI Reply rights holder; users receive a limited right of personal use."},
			{"8. Changes and termination", "A new version of this offer is published on this page. You may stop using the service, cancel a subscription, and request account deletion."},
			{"9. Contact", "Questions about this offer, payments, or an account may be sent to " + contactEmail + "."},
		},
		"uz": {
			{"1. Umumiy qoidalar", "Ushbu hujjat AI Reply raqamli xizmatidan foydalanish boʻyicha ommaviy ofertadir. Rozilik tugmasini bosish, roʻyxatdan oʻtish yoki xizmatdan foydalanish ofertani toʻliq qabul qilishni anglatadi."},
			{"2. Xizmat", "AI Reply messenjerlar uchun javob qoralamalarini tayyorlaydigan mobil ilova va klaviaturani taqdim etadi. Xizmat matnni avtomatik yaratadi va foydalanuvchi harakatisiz uni oluvchiga yubormaydi."},
			{"3. Hisob", "Kirish telefon raqami yoki elektron pochta orqali amalga oshiriladi. Foydalanuvchi qurilmasi va hisobiga kirish xavfsizligi uchun javob beradi."},
			{"4. Tarif, toʻlov va qaytarish", "Mavjud tarif, narx, muddat va limitlar xariddan oldin koʻrsatiladi. Toʻlov va qaytarish ilovalar doʻkoni yoki toʻlov provayderi qoidalari hamda majburiy qonun talablariga muvofiq bajariladi."},
			{"5. Foydalanish qoidalari", "Xizmatdan noqonuniy, zararli yoki chalgʻituvchi kontent yaratish va uchinchi shaxslar huquqlarini buzish uchun foydalanish mumkin emas. Jiddiy buzilishda kirish cheklanishi mumkin."},
			{"6. Javobgarlik", "Yaratilgan matn qoralamadir. Foydalanuvchi uni yuborishdan oldin tekshiradi va yuborilgan mazmun uchun oʻzi javob beradi. Xizmat uzluksiz yoki xatosiz ishlashi kafolatlanmaydi."},
			{"7. Maʼlumotlar va intellektual huquqlar", "Shaxsiy maʼlumotlar alohida Maxfiylik siyosatiga muvofiq qayta ishlanadi. Ilova, dizayn va dasturiy kod huquqlari AI Reply huquq egasiga tegishli; foydalanuvchiga shaxsiy foydalanish uchun cheklangan huquq beriladi."},
			{"8. Oʻzgartirish va bekor qilish", "Ofertaning yangi tahriri ushbu sahifada eʼlon qilinadi. Foydalanuvchi xizmatdan foydalanishni toʻxtatishi, obunani bekor qilishi va hisobni oʻchirishni soʻrashi mumkin."},
			{"9. Aloqa", "Oferta, toʻlov yoki hisob boʻyicha savollarni " + contactEmail + " manziliga yuborish mumkin."},
		},
	}
	return renderSections(sections, locale)
}

func privacyBody(locale string) string {
	sections := map[string][][2]string{
		"kk": {
			{"Не жинаймыз", "Есептік жазба деректері (телефон нөмірі не пошта), тариф, күндік санағыш, техникалық метадерек: платформа, қосымша нұсқасы, сұраныс уақыты мен ұзақтығы, токен саны."},
			{"Нені жинамаймыз", "Көшірілген хабарлама мәтіні, жасалған жауап, дауыстық танудың мазмұны, контакт тізімі, геолокация, жарнама идентификаторлары сақталмайды."},
			{"Мәтін қалай өңделеді", "Жауап жазу үшін мәтін серверде тек сол сұраныс кезінде жадта болады және AI провайдеріне жіберіледі. Ол дерекқорға да, журналға да жазылмайды."},
			{"Кім көре алады", "Әкімші панелінде хабарлама мазмұны жоқ — тек метадерек. Мұндай функция әдейі жасалмаған."},
			{"Деректі жою", "Есептік жазбаны жою сұранысы бойынша қолданушының жазбалары мен санағыштары жойылады."},
		},
		"ru": {
			{"Что мы собираем", "Данные аккаунта (телефон или почта), тариф, дневной счётчик, технические метаданные: платформа, версия приложения, время и длительность запроса, число токенов."},
			{"Что мы не собираем", "Текст скопированного сообщения, сгенерированный ответ, содержимое распознанной речи, список контактов, геолокацию и рекламные идентификаторы."},
			{"Как обрабатывается текст", "Для генерации текст находится в памяти сервера только во время запроса и передаётся AI-провайдеру. В базу и логи он не попадает."},
			{"Кто имеет доступ", "В админ-панели нет содержимого сообщений — только метаданные. Такая функция намеренно не реализована."},
			{"Удаление данных", "По запросу на удаление аккаунта записи и счётчики пользователя удаляются."},
		},
		"en": {
			{"What we collect", "Account data (phone or email), plan, daily counter, and technical metadata: platform, app version, request time and duration, token counts."},
			{"What we do not collect", "The copied message, the generated reply, speech-recognition content, contact lists, location, and advertising identifiers."},
			{"How text is processed", "To write a reply, the text stays in server memory for the duration of the request and is passed to the AI provider. It is not written to the database or the logs."},
			{"Who can see it", "The admin panel holds no message content — only metadata. That capability is deliberately absent."},
			{"Deleting data", "On an account deletion request, the user's records and counters are removed."},
		},
		"uz": {
			{"Nimalarni yigʻamiz", "Hisob maʼlumotlari (telefon yoki pochta), tarif, kunlik hisoblagich, texnik metamaʼlumot: platforma, ilova versiyasi, soʻrov vaqti va davomiyligi, token soni."},
			{"Nimalarni yigʻmaymiz", "Nusxalangan xabar matni, yaratilgan javob, nutq tanish mazmuni, kontaktlar roʻyxati, geolokatsiya va reklama identifikatorlari."},
			{"Matn qanday qayta ishlanadi", "Javob yozish uchun matn faqat soʻrov davomida server xotirasida boʻladi va AI provayderiga uzatiladi. U bazaga ham, jurnalga ham yozilmaydi."},
			{"Kim koʻra oladi", "Admin panelda xabar mazmuni yoʻq — faqat metamaʼlumot. Bunday funksiya ataylab yaratilmagan."},
			{"Maʼlumotlarni oʻchirish", "Hisobni oʻchirish soʻroviga koʻra foydalanuvchining yozuvlari va hisoblagichlari oʻchiriladi."},
		},
	}
	return renderSections(sections, locale)
}

func renderSections(all map[string][][2]string, locale string) string {
	sections, ok := all[locale]
	if !ok {
		sections = all["en"]
	}
	var b strings.Builder
	for _, s := range sections {
		b.WriteString("<h2>")
		b.WriteString(template.HTMLEscapeString(s[0]))
		b.WriteString("</h2><p>")
		b.WriteString(template.HTMLEscapeString(s[1]))
		b.WriteString("</p>")
	}
	return b.String()
}
