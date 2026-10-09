package web

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
)

// LEGAL TEXTS. The public offer and the privacy policy are rendered from
// here in four languages. Everything that depends on the deployment comes from
// the configuration: the operator (LEGAL_OPERATOR_NAME / _DETAILS, shown only
// when set — no entity is ever made up), the contact address (CONTACT_EMAIL;
// without it the support page is named instead, production requires it), the
// retention windows the server actually applies (RETENTION_*) and whether
// deleting an account revokes Sign in with Apple (APPLE_TEAM_ID / _KEY_ID /
// _PRIVATE_KEY; without the key the texts promise only what the person can do
// in their Apple ID settings). Bump legal.PrivacyVersion /
// legal.TermsVersion when the meaning of a text changes: the apps then ask
// every account to accept again before AI replies work.

// legalInfo — заң мәтіндеріне баптаудан келетін мәндер.
type legalInfo struct {
	OperatorName    string
	OperatorDetails string
	ContactEmail    string
	SupportURL      string
	DeletionURL     string
	Retention       config.Retention
	// NotificationDays — RETENTION_NOTIFICATIONS_DAYS (аяқталған жеткізулер).
	NotificationDays int
	// NotificationRowDays — хабарламалардың өзі: кемінде notifications.MinNotificationRetentionDays
	// (бір оқиғаға бір хабарлама кепілі); 0 — өшірілмейді.
	NotificationRowDays int
	// AppleRevocation — тіркелгі жойылғанда Sign in with Apple токені кері қайтарыла ма.
	AppleRevocation bool
}

func (s *Server) legalInfo() legalInfo {
	base := s.cfg.App.PublicBaseURL
	return legalInfo{
		OperatorName:        s.cfg.Legal.OperatorName,
		OperatorDetails:     s.cfg.Legal.OperatorDetails,
		ContactEmail:        s.cfg.App.ContactEmail(),
		SupportURL:          base + "/support",
		DeletionURL:         base + "/account/delete",
		Retention:           s.cfg.Retention,
		NotificationDays:    s.cfg.Push.RetentionDays,
		NotificationRowDays: notificationRowDays(s.cfg.Push.RetentionDays),
		AppleRevocation:     s.cfg.OAuth.AppleRevocation(),
	}
}

// notificationRowDays — хабарлама жолдары қанша күн сақталады (жеткізулерден ұзағырақ болуы мүмкін).
func notificationRowDays(days int) int {
	if days <= 0 {
		return 0
	}
	return max(days, notifications.MinNotificationRetentionDays)
}

// legalWords — тілге тәуелді шағын бөліктер (оператор, байланыс, мерзім).
type legalWords struct {
	operator    string // %s — LEGAL_OPERATOR_NAME
	details     string // %s — LEGAL_OPERATOR_DETAILS
	noOperator  string
	contact     string // %s — пошта
	contactPage string // %s — қолдау беті
	// writeTo / usePage — сөйлемнің соңы: «… хабарласыңыз».
	writeTo string
	usePage string
	// after — «N күннен кейін»; never — мерзімі 0 болғанда.
	after func(days int) string
	never string
}

var words = map[string]legalWords{
	"en": {
		operator: "The service is operated by %s.", details: "Operator details: %s.",
		noOperator: "In this policy “AI Reply”, “we” and “us” mean the operator of the service.",
		contact:    "Contact: %s.", contactPage: "Contact: the support page %s.",
		writeTo: "write to %s", usePage: "use the support page %s",
		after: func(n int) string {
			if n == 1 {
				return "after 1 day"
			}
			return fmt.Sprintf("after %d days", n)
		},
		never: "not removed automatically",
	},
	"ru": {
		operator: "Оператор сервиса — %s.", details: "Реквизиты: %s.",
		noOperator: "В этой политике «AI Reply» и «мы» означают оператора сервиса.",
		contact:    "Контакты: %s.", contactPage: "Контакты: страница поддержки %s.",
		writeTo: "напишите на %s", usePage: "воспользуйтесь страницей поддержки %s",
		after: func(n int) string { return fmt.Sprintf("через %d %s", n, russianDays(n)) },
		never: "не удаляются автоматически",
	},
	"kk": {
		operator: "Сервис операторы — %s.", details: "Деректемелері: %s.",
		noOperator: "Бұл саясатта «AI Reply» және «біз» деген сөздер сервис операторын білдіреді.",
		contact:    "Байланыс: %s.", contactPage: "Байланыс: %s қолдау беті.",
		writeTo: "%s мекенжайына жазыңыз", usePage: "%s қолдау бетін пайдаланыңыз",
		after: func(n int) string { return fmt.Sprintf("%d күннен кейін", n) },
		never: "автоматты түрде жойылмайды",
	},
	"uz": {
		operator: "Xizmat operatori — %s.", details: "Rekvizitlar: %s.",
		noOperator: "Ushbu siyosatda «AI Reply» va «biz» soʻzlari xizmat operatorini anglatadi.",
		contact:    "Aloqa: %s.", contactPage: "Aloqa: %s yordam sahifasi.",
		writeTo: "%s manziliga yozing", usePage: "%s yordam sahifasidan foydalaning",
		after: func(n int) string { return fmt.Sprintf("%d kundan keyin", n) },
		never: "avtomatik oʻchirilmaydi",
	},
}

// wordsFor — тілдің бөліктері; белгісіз тіл — ағылшынша.
func wordsFor(locale string) legalWords {
	if w, ok := words[locale]; ok {
		return w
	}
	return words["en"]
}

// russianDays — «день / дня / дней» санға қарай.
func russianDays(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return "день"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return "дня"
	default:
		return "дней"
	}
}

func (w legalWords) period(days int) string {
	if days <= 0 {
		return w.never
	}
	return w.after(days)
}

// who — оператор (берілсе) және байланыс сөйлемдері.
func (w legalWords) who(info legalInfo, withoutOperator bool) string {
	var parts []string
	switch {
	case info.OperatorName != "":
		parts = append(parts, fmt.Sprintf(w.operator, info.OperatorName))
	case withoutOperator:
		parts = append(parts, w.noOperator)
	}
	if info.OperatorDetails != "" {
		parts = append(parts, fmt.Sprintf(w.details, info.OperatorDetails))
	}
	if info.ContactEmail != "" {
		parts = append(parts, fmt.Sprintf(w.contact, info.ContactEmail))
	} else {
		parts = append(parts, fmt.Sprintf(w.contactPage, info.SupportURL))
	}
	return strings.Join(parts, " ")
}

// reach — «… мекенжайына жазыңыз» не «… қолдау бетін пайдаланыңыз».
func (w legalWords) reach(info legalInfo) string {
	if info.ContactEmail != "" {
		return fmt.Sprintf(w.writeTo, info.ContactEmail)
	}
	return fmt.Sprintf(w.usePage, info.SupportURL)
}

// contactValue — офертадағы байланыс: пошта не қолдау беті.
func (w legalWords) contactValue(locale string, info legalInfo) string {
	if info.ContactEmail != "" {
		return info.ContactEmail
	}
	switch locale {
	case "ru":
		return "страница поддержки " + info.SupportURL
	case "kk":
		return info.SupportURL + " қолдау беті"
	case "uz":
		return info.SupportURL + " yordam sahifasi"
	default:
		return "the support page " + info.SupportURL
	}
}

// offerer — офертаны ұсынушы (тек LEGAL_OPERATOR_NAME берілсе).
func offerer(locale string, info legalInfo) string {
	if info.OperatorName == "" {
		return ""
	}
	phrase := map[string]string{
		"en": " The offer is made by %s.", "ru": " Оферту предлагает %s.",
		"kk": " Офертаны ұсынушы — %s.", "uz": " Ofertani taklif qiluvchi — %s.",
	}[locale]
	out := fmt.Sprintf(phrase, info.OperatorName)
	if info.OperatorDetails != "" {
		out += " " + fmt.Sprintf(words[locale].details, info.OperatorDetails)
	}
	return out
}

func termsBody(locale string, info legalInfo) string {
	sections := map[string][][2]string{}
	for _, l := range []string{"kk", "ru", "en", "uz"} {
		contact, by := words[l].contactValue(l, info), offerer(l, info)
		switch l {
		case "kk":
			sections[l] = [][2]string{
				{"1. Жалпы ережелер", "Бұл құжат AI Reply цифрлық қызметін — мобильді қосымшаны, оның пернетақтасын және осы сайтты — пайдалану туралы жария оферта болып табылады. «Қабылдаймын» түймесін басу, тіркелу немесе қызметті пайдалану осы офертаның толық акцепті болып саналады." + by},
				{"2. Қызмет", "AI Reply — мессенджерлерге арналған жауап пен хабарлама жобаларын жасауға көмектесетін мобильді қосымша мен пернетақта. Мәтінді Құпиялық саясатында сипатталғандай OpenAI-дың AI моделі автоматты түрде жазады; қызмет мәтінді пайдаланушы әрекетінсіз алушыға жібермейді."},
				{"3. Есептік жазба", "Кіру Apple, Google арқылы немесе поштаға келетін бір реттік код арқылы орындалады. Пайдаланушы өз құрылғысы мен есептік жазбасына қолжетімділіктің қауіпсіздігіне жауап береді. Есептік жазбаны кез келген уақытта қосымшада немесе аккаунтты жою бетінде (" + info.DeletionURL + ") жоюға болады."},
				{"4. Тарифтер мен төлем", "Қазір қызмет тегін, күндік жауап лимитін оператор белгілейді; қанша жауап қалғанын қосымша мен пернетақта көрсетеді. Ақылы тарифтер енгізілсе, олардың бағасы, мерзімі мен лимиті сатып алу алдында көрсетіледі, ал оларды App Store не Google Play арқылы сол дүкеннің ережелері бойынша, соның ішінде төлем мен қайтару ережелері бойынша сатып алуға болады."},
				{"5. Пайдалану шарттары", "Қызметті заңсыз, зиянды, алдамшы контент жасауға немесе басқа тұлғалардың құқықтарын бұзуға пайдалануға болмайды. Елеулі бұзушылық кезінде қолжетімділік шектелуі мүмкін."},
				{"6. Жауапкершілік", "Жасалған мәтін — тек жоба. AI қателесуі мүмкін: деректерді, аттарды, бағалар мен күндерді тексеріңіз. Пайдаланушы мәтінді жіберер алдында тексереді және жіберілген мазмұнға өзі жауап береді. Қызмет үздіксіз немесе қатесіз жұмыс істейтініне кепілдік берілмейді."},
				{"7. Деректер мен зияткерлік құқықтар", "Дербес деректер бөлек Құпиялық саясатына сәйкес өңделеді. Қосымшаға, дизайнға және бағдарламалық кодқа құқықтар AI Reply құқық иесіне тиесілі; пайдаланушыға жеке пайдалану үшін шектеулі құқық беріледі."},
				{"8. Өзгерту және тоқтату", "Офертаның жаңа редакциясы осы бетте жарияланады. Ол AI өңдеу ережелерін өзгертсе, қосымша оны қайта қабылдауды сұрайды. Пайдаланушы қызметті пайдалануды кез келген уақытта тоқтатып, есептік жазбасын жоя алады; офертаны елеулі бұзған жағдайда қолжетімділік шектелуі мүмкін."},
				{"9. Байланыс", "Оферта немесе есептік жазба бойынша сұрақтар: " + contact + "."},
			}
		case "ru":
			sections[l] = [][2]string{
				{"1. Общие положения", "Настоящий документ является публичной офертой на использование цифрового сервиса AI Reply: мобильного приложения, его клавиатуры и этого сайта. Нажатие кнопки согласия, регистрация или использование сервиса означают полный и безоговорочный акцепт оферты." + by},
				{"2. Предмет оферты", "AI Reply предоставляет мобильное приложение и клавиатуру для подготовки черновиков ответов и сообщений в мессенджерах. Тексты автоматически пишет AI-модель OpenAI, как описано в Политике конфиденциальности; сервис не отправляет их получателю без действия пользователя."},
				{"3. Аккаунт", "Вход выполняется через Apple, Google или по одноразовому коду, который приходит на почту. Пользователь отвечает за сохранность доступа к своему устройству и аккаунту. Удалить аккаунт можно в любой момент в приложении или на странице удаления аккаунта (" + info.DeletionURL + ")."},
				{"4. Тарифы и оплата", "Сейчас сервис бесплатный, с дневным лимитом ответов, который устанавливает оператор; приложение и клавиатура показывают, сколько ответов осталось. Если появятся платные тарифы, их цена, срок и лимиты будут показаны до покупки, а купить их можно будет через App Store или Google Play по правилам этого магазина, включая правила оплаты и возврата."},
				{"5. Правила использования", "Запрещено использовать сервис для незаконного, вредоносного или вводящего в заблуждение контента и нарушения прав третьих лиц. При существенном нарушении доступ может быть ограничен."},
				{"6. Ответственность", "Сгенерированный текст является черновиком. AI может ошибаться: проверяйте факты, имена, цены и даты. Пользователь проверяет текст до отправки и самостоятельно отвечает за отправленное содержание. Бесперебойная и безошибочная работа сервиса не гарантируется."},
				{"7. Данные и интеллектуальные права", "Персональные данные обрабатываются по отдельной Политике конфиденциальности. Права на приложение, дизайн и программный код принадлежат правообладателю AI Reply; пользователю предоставляется ограниченное право личного использования."},
				{"8. Изменение и прекращение", "Новая редакция оферты публикуется на этой странице. Если она меняет правила AI-обработки, приложение попросит принять её снова. Пользователь может в любой момент прекратить использование сервиса и удалить аккаунт; при существенном нарушении оферты доступ может быть ограничен."},
				{"9. Контакты", "Вопросы по оферте или аккаунту: " + contact + "."},
			}
		case "en":
			sections[l] = [][2]string{
				{"1. General", "This document is a public offer governing use of the AI Reply digital service: the mobile app, its keyboard and this website. Selecting the consent control, registering, or using the service constitutes full acceptance of this offer." + by},
				{"2. Service", "AI Reply provides a mobile app and keyboard that prepare draft replies and messages for messengers. The texts are written automatically by an AI model of OpenAI, as described in the Privacy Policy, and are never sent to a recipient without a user action."},
				{"3. Account", "You sign in with Apple, Google or a one-time code sent to your e-mail. You are responsible for keeping access to your device and account secure. You can delete the account at any time in the app or on the account deletion page (" + info.DeletionURL + ")."},
				{"4. Plans and payment", "The service is currently free, with a daily reply limit set by the operator; the app and the keyboard show how many replies are left. If paid plans are introduced, their price, term and limits are shown before purchase, and they are bought through the App Store or Google Play under the rules of that store, including its payment and refund rules."},
				{"5. Acceptable use", "The service must not be used for illegal, harmful, or deceptive content or to violate third-party rights. Access may be restricted for a material violation."},
				{"6. Responsibility", "Generated text is a draft. AI can be wrong: check facts, names, prices and dates. You review the text before sending it and remain responsible for what you send. Uninterrupted or error-free operation is not guaranteed."},
				{"7. Data and intellectual property", "Personal data is handled under the separate Privacy Policy. Rights in the app, design, and software belong to the AI Reply rights holder; users receive a limited right of personal use."},
				{"8. Changes and termination", "A new version of this offer is published on this page. If it changes the rules for AI processing, the app asks you to accept it again. You may stop using the service and delete your account at any time; access may be restricted for a material violation of this offer."},
				{"9. Contact", "Questions about this offer or your account: " + contact + "."},
			}
		case "uz":
			sections[l] = [][2]string{
				{"1. Umumiy qoidalar", "Ushbu hujjat AI Reply raqamli xizmatidan — mobil ilova, uning klaviaturasi va ushbu saytdan — foydalanish boʻyicha ommaviy ofertadir. Rozilik tugmasini bosish, roʻyxatdan oʻtish yoki xizmatdan foydalanish ofertani toʻliq qabul qilishni anglatadi." + by},
				{"2. Xizmat", "AI Reply messenjerlar uchun javob va xabar qoralamalarini tayyorlaydigan mobil ilova va klaviaturani taqdim etadi. Matnlarni Maxfiylik siyosatida tavsiflanganidek OpenAI’ning AI modeli avtomatik yozadi; xizmat ularni foydalanuvchi harakatisiz oluvchiga yubormaydi."},
				{"3. Hisob", "Kirish Apple, Google orqali yoki pochtaga keladigan bir martalik kod orqali amalga oshiriladi. Foydalanuvchi qurilmasi va hisobiga kirish xavfsizligi uchun javob beradi. Hisobni istalgan vaqtda ilovada yoki hisobni oʻchirish sahifasida (" + info.DeletionURL + ") oʻchirish mumkin."},
				{"4. Tarif va toʻlov", "Hozir xizmat bepul, kunlik javoblar limitini operator belgilaydi; qancha javob qolganini ilova va klaviatura koʻrsatadi. Agar pullik tariflar joriy etilsa, ularning narxi, muddati va limitlari xariddan oldin koʻrsatiladi, ularni esa App Store yoki Google Play orqali shu doʻkon qoidalariga, jumladan toʻlov va qaytarish qoidalariga muvofiq sotib olish mumkin boʻladi."},
				{"5. Foydalanish qoidalari", "Xizmatdan noqonuniy, zararli yoki chalgʻituvchi kontent yaratish va uchinchi shaxslar huquqlarini buzish uchun foydalanish mumkin emas. Jiddiy buzilishda kirish cheklanishi mumkin."},
				{"6. Javobgarlik", "Yaratilgan matn qoralamadir. AI xato qilishi mumkin: faktlar, ismlar, narxlar va sanalarni tekshiring. Foydalanuvchi matnni yuborishdan oldin tekshiradi va yuborilgan mazmun uchun oʻzi javob beradi. Xizmat uzluksiz yoki xatosiz ishlashi kafolatlanmaydi."},
				{"7. Maʼlumotlar va intellektual huquqlar", "Shaxsiy maʼlumotlar alohida Maxfiylik siyosatiga muvofiq qayta ishlanadi. Ilova, dizayn va dasturiy kod huquqlari AI Reply huquq egasiga tegishli; foydalanuvchiga shaxsiy foydalanish uchun cheklangan huquq beriladi."},
				{"8. Oʻzgartirish va bekor qilish", "Ofertaning yangi tahriri ushbu sahifada eʼlon qilinadi. Agar u AI ishlov berish qoidalarini oʻzgartirsa, ilova uni qayta qabul qilishni soʻraydi. Foydalanuvchi istalgan vaqtda xizmatdan foydalanishni toʻxtatishi va hisobini oʻchirishi mumkin; oferta jiddiy buzilganda kirish cheklanishi mumkin."},
				{"9. Aloqa", "Oferta yoki hisob boʻyicha savollar: " + contact + "."},
			}
		}
	}
	return renderSections(sections, locale)
}

func privacyBody(locale string, info legalInfo) string {
	r := info.Retention
	sections := map[string][][2]string{}
	for _, l := range []string{"kk", "ru", "en", "uz"} {
		w := words[l]
		who, reach := w.who(info, true), w.reach(info)
		p := w.period
		// Жеткізулер мен хабарламалардың мерзімі бірдей болса — бір жол, әйтпесе екеуі бөлек.
		notices := func(both, deliveries, rows string) string {
			if info.NotificationRowDays == info.NotificationDays {
				return both + " — " + p(info.NotificationDays)
			}
			return deliveries + " — " + p(info.NotificationDays) + "; " + rows + " — " + p(info.NotificationRowDays)
		}
		// Apple: кері қайтару кілті бар болса ғана «ажыратамыз» дейміз.
		apple := func(revoked, manual string) string {
			if info.AppleRevocation {
				return revoked
			}
			return manual
		}
		switch l {
		case "kk":
			sections[l] = [][2]string{
				{"Біз кімбіз", "Бұл саясат iPhone мен Android-қа арналған AI Reply қосымшасында, оның пернетақтасында және осы сайтта дербес деректер қалай өңделетінін түсіндіреді. " + who},
				{"Қандай деректер жинаймыз", strings.Join([]string{
					"Аккаунт: пошта мекенжайы; Apple не Google арқылы кірсеңіз — олар беретін аккаунт идентификаторы мен олар бөлісетін пошта (Apple-де бұл жасырын жіберу мекенжайы болуы мүмкін); атыңыз, егер оны өзіңіз енгізсеңіз не Apple немесе Google берсе.",
					"Профиль: қосымшада толтыратыныңыз — рөл, өзіңіз туралы қысқаша сипаттама, қалаған тон, бизнестің не ұсынатыны, сипаттамасы мен ережелері, орысша жауаптардағы грамматикалық тек, жауап тілі, таңдалған қосымша тілі, локаль және уақыт белдеуі.",
					"Құрылғы және орнату: қосымша жасайтын кездейсоқ құрылғы және орнату идентификаторлары, платформа, жүйе нұсқасы, қосымша нұсқасы, құрылғы моделі мен өндірушісі, push-токен (шифрланып сақталады) және хабарламаларға рұқсат бар-жоғы.",
					"Қолданыс: күндік және айлық жауап санағыштары және әр сұраныс бойынша тек метадерек — уақыты, ұзақтығы, токен саны, болжамды құны, қате коды, тілі, мәтін ұзындығы, режимі (жауап, жазу не түзету) және тариф.",
					"Қосымша оқиғалары: онбординг пен баптау қадамдары, мысалы «пернетақта қосылды», — мәтінсіз.",
					"Келісім жазбалары: шарттар мен осы саясаттың қай нұсқасын, қашан, қай платформа мен қосымша нұсқасынан қабылдағаныңыз, сондай-ақ AI өңдеуге келісімді қашан кері қайтарғаныңыз.",
					"Шағымдар: жауапқа шағымдансаңыз — себеп, пікіріңіз және «Жауап мәтінін шағыммен бірге жіберу» қосулы қалса ғана, жасалған мәтін.",
					"Төлемдер: сатып алу жазбалары — ақылы тарифтер сатылса ғана; сатып алу App Store не Google Play арқылы өтеді.",
				}, "\n")},
				{"AI жауаптары қалай жазылады", strings.Join([]string{
					"«Жауап беру» батырмасын басқанда көшірілген хабарлама не ағымдағы өрісте өзіңіз белгілеген мәтін (пернетақта оны тек осы сәтте оқиды), нұсқауыңыз, профиль деректері және таңдалған үлгі біздің сервер арқылы OpenAI-ға (АҚШ) жіберіледі, мәтінді соның моделі жазады. «Жазу» батырмасын басқанда тек нұсқауыңыз бен грамматикалық тегіңіз жіберіледі; көшірілген хабарлама «Көшірілгенге жауап беру» таңдасаңыз ғана, жауап ретінде жіберіледі. Ақылды түзету қосулы болса, AI Reply-дың өз өрісінде терген нұсқауыңыз түзету ұсыну үшін теру кезінде де жіберіледі.",
					"Біздің сервер бұл мәтіндерді сұранысты өңдеу кезінде ғана жадта ұстайды, оларды дерекқорға да, журналға да жазбайды. OpenAI-ға олар сақтау өшірілген API арқылы (store=false) жіберіледі; OpenAI оларды өзінің API ережелері бойынша өңдейді: мысалы, теріс пайдалануды анықтау үшін шектеулі уақыт сақтай алады және әдепкіде API деректерін модельдерді үйретуге пайдаланбайды.",
					"Бұл тек келісім экранында келісім бергеннен кейін болады. Келісімді кез келген уақытта Параметрлер → Құпиялылық бөлімінде кері қайтаруға болады.",
					"Android-та нұсқауды дауыспен айтуға болады. Сөз мүмкін болса телефонның өзінде, болмаса телефонның сөйлеуді тану қызметімен (әдетте Google) танылады; AI Reply тек танылған мәтінді алады, дауыс жазбасын емес.",
					"iPhone қосымшасында жауап берілетін хабарламаны, профильді не үлгіні дауыспен айтуға болады. Сөз мүмкін болса iPhone-ның өзінде Apple сөйлеуді тану жүйесімен, болмаса Apple сөйлеуді тану қызметінде танылады; AI Reply тек танылған мәтінді алады, дауыс жазбасын емес.",
				}, "\n")},
				{"Нені жинамаймыз", "Біз контактілерді, геолокацияны және жарнама идентификаторларын жинамаймыз және сізді басқа қосымшаларда бақыламаймыз. Басқа қосымшаларда тергеніңізден ештеңе жіберілмейді, тек «Жауап беру» басқанда көшірілген не өзіңіз белгілеген хабарлама және AI Reply-дың өз өрісінде терген нұсқауыңыз жіберіледі. Алмасу буфері мен белгіленген мәтін оларды қолданатын әрекетті басқанда ғана оқылады, мысалы «Жауап беру», «Көшірілгенге жауап беру», үлгі не қою батырмасы. Құпиясөздер мен қорғалған өрістер ешқашан өңделмейді."},
				{"Біздің атымыздан деректерді кім өңдейді", "OpenAI — жауаптарды, хабарламаларды және түзетулерді жазады. Google — Google арқылы кіру, Firebase Cloud Messaging (екі платформадағы хабарламалар) және Android-та, егер қолданылса, телефонның сөйлеуді тану қызметі. Apple — Apple арқылы кіру, iPhone-ға хабарлама жеткізу (Firebase арқылы Apple Push Notification service) және iPhone қосымшасында дауыспен айтсаңыз, Apple сөйлеуді тану жүйесі. Resend — кіру кодтары мен аккаунт хаттарын жібереді. Серверіміздің хостинг провайдері — дерекқорды сақтайды. Әрқайсысы тек өз міндетіне қажеттіні алады."},
				{"Деректерді басқа елдерге беру", "OpenAI, Google, Apple және Resend деректерді АҚШ-та және басқа елдерде өңдейді. Сондықтан оларға берілген деректер сіздің еліңізден тыс шығуы мүмкін; олар деректерді өздерінің деректерді қорғау және қауіпсіздік міндеттемелеріне сәйкес өңдейді."},
				{"Деректер қанша сақталады", "Аккаунт деректері — пошта мен кіру тәсілдері, профиль мен баптаулар, келісім жазбалары, құрылғылар, санағыштар, шағымдар, хабарлама баптаулары, сондай-ақ сізге жеке жіберілген хабарламаның алушылар тізіміндегі поштаңыз не аккаунт нөміріңіз — аккаунт жойылғанға дейін сақталады. Басқа деректер автоматты түрде жойылады: " + strings.Join([]string{
					"сұраныс метадеректері — " + p(r.AIUsageEventsDays),
					"қосымша оқиғалары — " + p(r.ProductEventsDays),
					"кіру және жою кодтары (берілген сәттен) — " + p(r.OTPDays),
					"аяқталған сессиялар (мерзімі біткеннен не шыққаннан бастап) — " + p(r.RefreshTokenDays),
					"аккаунтқа байланбаған қосымша орнатуы (соңғы рет көрінгеннен бастап) — " + p(r.AnonInstallationsDays),
					notices("аяқталған хабарламалар мен жеткізулер", "аяқталған хабарлама жеткізулері", "хабарламалардың өзі"),
				}, "; ") + ". Әкімшінің аккаунтқа қатысты әрекеттерінің журналы (мысалы, бұғаттау не тарифті ауыстыру) аккаунттың кездейсоқ нөмірін поштаңызсыз сақтайды және автоматты түрде жойылмайды, аккаунт жойылғаннан кейін де. Көшірілген хабарламалар, нұсқаулар және жасалған жауаптар мүлде сақталмайды, тек өзіңіз жіберген шағымдағы жауап мәтіні қалады."},
				{"Қолжетімділік және қауіпсіздік", "Әкімші панеліне тек уәкілетті әкімшілер кіре алады. Онда поштасының бір бөлігі жасырылған аккаунттар, тарифтер, санағыштар, сұраныс метадеректері және жіберілген шағымдар көрінеді; сұраныс мәтіндерінің экраны жоқ, өйткені олар сақталмайды. Байланыстар шифрланады, push-токендер шифрланып, кіру кодтары тек хэш түрінде сақталады."},
				{"Сіздің таңдауыңыз бен құқықтарыңыз", "Қосымшада профиліңізді көріп, өзгерте аласыз, хабарлама санаттарын қосып-өшіре аласыз (жарнама хабарламалары өзіңіз қоспайынша өшірулі) және Параметрлер → Құпиялылық бөлімінде AI өңдеуге келісімді кері қайтара аласыз: қайта келісім бергенше AI жауаптары жұмыс істемейді, аккаунт сақталады. Аккаунтты Параметрлер → Тіркелгі → Тіркелгіні жою бөлімінде немесе " + info.DeletionURL + " бетінде жоюға болады — ол бірден жойылады. " + apple(
					"Apple арқылы кірген болсаңыз, iPhone қосымшасында аккаунтты жойғанда Apple-ден Apple арқылы кіруді ажыратуды да сұраймыз; AI Reply-ды Apple ID баптауларындағы «Apple арқылы кіру» бөлімінен өзіңіз де кез келген уақытта алып тастай аласыз.",
					"Apple арқылы кірген болсаңыз, AI Reply-ды Apple ID баптауларындағы «Apple арқылы кіру» бөлімінен алып тастай аласыз.") + " Деректеріңіздің көшірмесін алу, оларды түзету немесе сұрақ қою үшін " + reach + "."},
				{"Балалар", "AI Reply 13 жасқа толмаған балаларға арналмаған."},
				{"Саясаттың өзгеруі", "Саясаттың жаңа редакциясы осы бетте күнімен жарияланады. Өзгеріс мәтіндерді өңдеуге қатысты болса, AI жауаптары қайта жұмыс істеуі үшін қосымша жаңа нұсқаны қабылдауды сұрайды."},
			}
		case "ru":
			sections[l] = [][2]string{
				{"Кто мы", "Эта политика объясняет, как обрабатываются персональные данные в приложении AI Reply для iPhone и Android, его клавиатуре и на этом сайте. " + who},
				{"Какие данные мы собираем", strings.Join([]string{
					"Аккаунт: адрес почты; при входе через Apple или Google — идентификатор аккаунта, который они передают, и почта, которой они делятся (у Apple это может быть скрытый адрес для пересылки); имя, если вы его укажете или его передаст Apple или Google.",
					"Профиль: то, что вы заполняете в приложении, — роль, краткое описание, предпочитаемый тон, что предлагает ваш бизнес, его описание и правила, грамматический род для ответов на русском, язык ответов, выбранный язык приложения, локаль и часовой пояс.",
					"Устройство и установка: случайные идентификаторы устройства и установки, которые создаёт приложение, платформа, версия системы, версия приложения, модель и производитель устройства, push-токен (хранится в зашифрованном виде) и разрешены ли уведомления.",
					"Использование: дневной и месячный счётчики ответов и по каждому запросу только метаданные — время, длительность, число токенов, расчётная стоимость, код ошибки, язык, длина текста, режим (ответ, написание или коррекция) и тариф.",
					"События приложения: шаги онбординга и настроек, например «клавиатура включена», — без текста.",
					"Записи о согласии: какие версии условий и этой политики вы приняли, когда, с какой платформы и версии приложения, а также когда вы отозвали согласие на AI-обработку.",
					"Жалобы: если вы пожалуетесь на ответ — причина, ваш комментарий и, только если вы оставили включённым «Отправить текст ответа вместе с жалобой», сгенерированный текст.",
					"Платежи: записи о покупках — только если платные тарифы когда-либо будут продаваться; покупка будет проходить через App Store или Google Play.",
				}, "\n")},
				{"Как пишутся AI-ответы", strings.Join([]string{
					"Когда вы нажимаете «Ответить», скопированное сообщение или текст, который вы выделили в текущем поле (клавиатура читает его только в этот момент), ваша инструкция, данные профиля и выбранный шаблон передаются через наш сервер в OpenAI (США), модель которой пишет текст. Когда вы нажимаете «Написать», отправляются только ваша инструкция и грамматический род; скопированное сообщение отправляется, только если вы выберете «Ответить на скопированное», — тогда это обычный ответ. Если включена умная коррекция, инструкция, которую вы набираете в собственном поле AI Reply, отправляется и во время набора, чтобы предложить исправление.",
					"Наш сервер держит эти тексты только в памяти на время обработки запроса и не записывает их ни в базу, ни в логи. В OpenAI они передаются через API с отключённым хранением (store=false); OpenAI обрабатывает их по своим правилам для API: например, может хранить ограниченное время для выявления злоупотреблений и по умолчанию не использует данные API для обучения моделей.",
					"Это происходит только после того, как вы дали согласие на экране согласия. Отозвать его можно в любой момент в разделе Настройки → Конфиденциальность.",
					"На Android инструкцию можно надиктовать. Речь распознаётся на самом телефоне, если это возможно, иначе — системной службой распознавания речи (обычно Google); AI Reply получает только распознанный текст, но не запись голоса.",
					"В приложении для iPhone можно надиктовать сообщение, на которое нужно ответить, профиль или шаблон. Речь распознаёт система распознавания речи Apple на самом iPhone, если это возможно, иначе — сервис распознавания речи Apple; AI Reply получает только распознанный текст, но не запись голоса.",
				}, "\n")},
				{"Что мы не собираем", "Мы не собираем контакты, геолокацию и рекламные идентификаторы и не отслеживаем вас в других приложениях. Из того, что вы набираете в других приложениях, ничего не отправляется, кроме скопированного или выделенного вами сообщения, когда вы нажимаете «Ответить», и инструкции в собственном поле AI Reply. Буфер обмена и выделенный текст читаются только когда вы нажимаете действие, которое их использует, например «Ответить», «Ответить на скопированное», шаблон или кнопку вставки. Пароли и защищённые поля никогда не обрабатываются."},
				{"Кто обрабатывает данные по нашему поручению", "OpenAI — пишет ответы, сообщения и исправления. Google — вход через Google, Firebase Cloud Messaging (уведомления на обеих платформах) и на Android — служба распознавания речи телефона, если она используется. Apple — вход через Apple, доставка уведомлений на iPhone (Apple Push Notification service через Firebase) и, если вы диктуете в приложении для iPhone, распознавание речи Apple. Resend — отправляет коды входа и письма об аккаунте. Хостинг-провайдер нашего сервера — хранит базу данных. Каждый из них получает только то, что нужно для его задачи."},
				{"Передача данных в другие страны", "OpenAI, Google, Apple и Resend обрабатывают данные в США и других странах. Поэтому переданные им данные могут покидать вашу страну; они обрабатывают их в соответствии со своими обязательствами по защите данных и безопасности."},
				{"Сколько хранятся данные", "Данные аккаунта — почта и способы входа, профиль и настройки, записи о согласии, устройства, счётчики, жалобы, настройки уведомлений, а также ваша почта или номер аккаунта в списке получателей уведомления, отправленного лично вам, — хранятся до удаления аккаунта. Остальные данные удаляются автоматически: " + strings.Join([]string{
					"метаданные запросов — " + p(r.AIUsageEventsDays),
					"события приложения — " + p(r.ProductEventsDays),
					"коды входа и удаления (с момента выдачи) — " + p(r.OTPDays),
					"завершённые сессии (с окончания срока или выхода) — " + p(r.RefreshTokenDays),
					"установка приложения без привязки к аккаунту (с последнего появления) — " + p(r.AnonInstallationsDays),
					notices("завершённые уведомления и доставки", "завершённые доставки уведомлений", "сами уведомления"),
				}, "; ") + ". Журнал действий администраторов с аккаунтом (например, блокировка или смена тарифа) хранит случайный номер аккаунта без вашей почты и не удаляется автоматически, в том числе после удаления аккаунта. Скопированные сообщения, инструкции и сгенерированные ответы не хранятся вовсе, кроме текста ответа в жалобе, которую вы решили отправить."},
				{"Доступ и безопасность", "В админ-панель могут войти только уполномоченные администраторы. Там видны аккаунты со скрытой частью почты, тарифы, счётчики, метаданные запросов и присланные жалобы; экрана с текстами запросов нет, потому что они не хранятся. Соединения шифруются, push-токены хранятся в зашифрованном виде, коды входа — только в виде хэшей."},
				{"Ваш выбор и права", "В приложении можно посмотреть и изменить профиль, включить или выключить категории уведомлений (рекламные выключены, пока вы их не включите) и отозвать согласие на AI-обработку в разделе Настройки → Конфиденциальность: AI-ответы перестанут работать, пока вы снова не дадите согласие, а аккаунт сохранится. Удалить аккаунт можно в разделе Настройки → Аккаунт → Удалить аккаунт или на странице " + info.DeletionURL + " — он удаляется сразу. " + apple(
					"Если вы входили через Apple, при удалении аккаунта в приложении для iPhone мы также просим Apple отключить вход с Apple; удалить AI Reply самостоятельно можно в любой момент в настройках Apple ID в разделе «Вход с Apple».",
					"Если вы входили через Apple, AI Reply можно удалить в настройках Apple ID в разделе «Вход с Apple».") + " Чтобы получить копию своих данных, исправить их или задать вопрос, " + reach + "."},
				{"Дети", "AI Reply не предназначен для детей младше 13 лет."},
				{"Изменения политики", "Новая редакция политики публикуется на этой странице с датой. Если изменение касается обработки текстов, приложение попросит принять новую версию, прежде чем AI-ответы снова заработают."},
			}
		case "en":
			sections[l] = [][2]string{
				{"Who we are", "This policy explains how personal data is processed in the AI Reply app for iPhone and Android, its keyboard and on this website. " + who},
				{"What we collect", strings.Join([]string{
					"Account: your e-mail address; if you sign in with Apple or Google, the account identifier they give us and the e-mail they share (with Apple this can be a private relay address); your name, if you enter it or Apple or Google shares it.",
					"Profile: what you fill in in the app — role, a short description of yourself, preferred tone, what your business offers, its summary and rules, the grammatical gender for replies in Russian, reply language, the app language you choose, locale and time zone.",
					"Device and installation: random device and installation identifiers created by the app, platform, operating-system version, app version, device model and manufacturer, the push token (stored encrypted) and whether notifications are allowed.",
					"Usage: daily and monthly reply counters and, for each request, metadata only — time, duration, token counts, estimated cost, error code, language, text length, mode (reply, write or correction) and plan.",
					"App events: onboarding and settings steps such as “keyboard enabled” — never text.",
					"Consent records: which versions of the terms and of this policy you accepted, when, from which platform and app version, and when you withdrew consent to AI processing.",
					"Reports: when you report a reply — the reason, your comment and, only if you leave “Send the reply text with the report” on, the generated text.",
					"Payments: purchase records — only if paid plans are ever sold; they would be bought through the App Store or Google Play.",
				}, "\n")},
				{"How AI replies are written", strings.Join([]string{
					"When you tap Reply, the message you copied or the text you selected in the current field (the keyboard reads it only at that moment), your instruction, your profile details and the template you chose go through our server to OpenAI (USA), whose model writes the text. When you tap Write, only your instruction and your grammatical gender are sent; the copied message goes only if you choose Reply to copied, and then as a reply. With Smart correction on, the instruction you type in AI Reply's own field is also sent while you type it, to suggest a correction.",
					"Our server keeps these texts only in memory while it handles the request and does not write them to the database or the logs. They are sent to OpenAI through its API with storage turned off (store=false); OpenAI processes them under its own API policies: for example, it may keep them for a limited time to detect abuse, and by default it does not use API data to train its models.",
					"This happens only after you accept it on the consent screen. You can withdraw that consent at any time in Settings → Privacy.",
					"On Android you can dictate the instruction. Speech is recognised on the phone when possible, otherwise by the phone's speech recognition service (usually Google); AI Reply receives only the recognised text, never the audio.",
					"In the iPhone app you can dictate a message to reply to, your profile or a template. Speech is recognised by Apple speech recognition on the iPhone when possible, otherwise by Apple's speech recognition service; AI Reply receives only the recognised text, never the audio.",
				}, "\n")},
				{"What we do not collect", "We do not collect contacts, location or advertising identifiers, and we do not track you across other apps. Nothing else you type in other apps is sent: only the message you copied or selected yourself, when you tap Reply, and the instruction you type in AI Reply's own field. The clipboard and the selected text are read only when you tap an action that uses them, such as Reply, Reply to copied, a template or the paste button. Passwords and secure fields are never processed."},
				{"Who processes data for us", "OpenAI — writes replies, messages and corrections. Google — Sign in with Google, Firebase Cloud Messaging (notifications on both platforms) and, on Android, the phone's speech recognition service if it is used. Apple — Sign in with Apple, delivery of notifications to iPhone (Apple Push Notification service, through Firebase) and, if you dictate in the iPhone app, Apple speech recognition. Resend — sends sign-in codes and account e-mails. The hosting provider of our server — stores the database. Each of them receives only what its task needs."},
				{"Transfers to other countries", "OpenAI, Google, Apple and Resend process data in the USA and other countries. Data sent to them may therefore leave your country; they process it under their own data protection and security commitments."},
				{"How long we keep data", "Account data — e-mail and sign-in methods, profile and settings, consent records, devices, counters, reports, notification settings and your e-mail or account number in the recipient list of a notification sent to you personally — is kept until you delete the account. Other data is removed automatically: " + strings.Join([]string{
					"request metadata — " + p(r.AIUsageEventsDays),
					"app events — " + p(r.ProductEventsDays),
					"sign-in and deletion codes (from when they are issued) — " + p(r.OTPDays),
					"ended sessions (from expiry or sign-out) — " + p(r.RefreshTokenDays),
					"an app installation not linked to an account (from when it was last seen) — " + p(r.AnonInstallationsDays),
					notices("finished notifications and deliveries", "finished notification deliveries", "the notifications themselves"),
				}, "; ") + ". A log of administrator actions on an account (for example a block or a plan change) keeps the random account number, without your e-mail, and is not removed automatically, including after the account is deleted. Copied messages, instructions and generated replies are not kept at all, except the reply text in a report you choose to send."},
				{"Access and security", "Only authorised administrators can sign in to the admin panel. It shows accounts with a partly hidden e-mail, plans, counters, request metadata and the reports people send; it has no screen for the texts of requests, because they are not stored. Connections are encrypted, push tokens are stored encrypted and sign-in codes only as hashes."},
				{"Your choices and rights", "In the app you can see and change your profile, turn notification categories on or off (marketing is off until you turn it on) and withdraw consent to AI processing in Settings → Privacy: AI replies then stop until you accept again, and the account stays. You can delete your account in Settings → Account → Delete account or on the page " + info.DeletionURL + "; it is deleted right away. " + apple(
					"If you signed in with Apple, deleting the account in the iPhone app also asks Apple to disconnect Sign in with Apple; you can always remove AI Reply yourself in your Apple ID settings under Sign in with Apple.",
					"If you signed in with Apple, you can remove AI Reply in your Apple ID settings under Sign in with Apple.") + " To get a copy of your data, correct it or ask a question, " + reach + "."},
				{"Children", "AI Reply is not intended for children under 13."},
				{"Changes to this policy", "A new version of this policy is published on this page with its date. When a change affects how texts are processed, the app asks you to accept the new version before AI replies work again."},
			}
		case "uz":
			sections[l] = [][2]string{
				{"Biz kimmiz", "Ushbu siyosat iPhone va Android uchun AI Reply ilovasida, uning klaviaturasida va ushbu saytda shaxsiy maʼlumotlar qanday qayta ishlanishini tushuntiradi. " + who},
				{"Qanday maʼlumotlarni yigʻamiz", strings.Join([]string{
					"Hisob: pochta manzili; Apple yoki Google orqali kirsangiz — ular beradigan hisob identifikatori va ular ulashadigan pochta (Apple’da bu yashirin uzatish manzili boʻlishi mumkin); ismingiz, agar uni oʻzingiz kiritsangiz yoki Apple yoki Google bersa.",
					"Profil: ilovada toʻldiradiganingiz — rol, oʻzingiz haqingizda qisqa tavsif, afzal koʻrgan ohang, biznesingiz nimani taklif qilishi, uning tavsifi va qoidalari, rus tilidagi javoblar uchun grammatik jins, javob tili, tanlangan ilova tili, lokal va vaqt mintaqasi.",
					"Qurilma va oʻrnatish: ilova yaratadigan tasodifiy qurilma va oʻrnatish identifikatorlari, platforma, tizim versiyasi, ilova versiyasi, qurilma modeli va ishlab chiqaruvchisi, push-token (shifrlangan holda saqlanadi) va bildirishnomalarga ruxsat bor-yoʻqligi.",
					"Foydalanish: kunlik va oylik javob hisoblagichlari va har bir soʻrov boʻyicha faqat metamaʼlumot — vaqt, davomiylik, token soni, taxminiy narx, xato kodi, til, matn uzunligi, rejim (javob, yozish yoki tuzatish) va tarif.",
					"Ilova hodisalari: onboarding va sozlash qadamlari, masalan «klaviatura yoqildi», — matnsiz.",
					"Rozilik yozuvlari: shartlar va ushbu siyosatning qaysi versiyalarini, qachon, qaysi platforma va ilova versiyasidan qabul qilganingiz, shuningdek AI ishlov berishga rozilikni qachon qaytarib olganingiz.",
					"Shikoyatlar: javob ustidan shikoyat qilsangiz — sabab, izohingiz va faqat «Javob matnini shikoyat bilan birga yuborish» yoqilgan qolsa, yaratilgan matn.",
					"Toʻlovlar: xaridlar yozuvlari — faqat pullik tariflar sotilsa; xarid App Store yoki Google Play orqali amalga oshiriladi.",
				}, "\n")},
				{"AI javoblari qanday yoziladi", strings.Join([]string{
					"«Javob berish» tugmasini bosganingizda nusxalangan xabar yoki joriy maydonda oʻzingiz belgilagan matn (klaviatura uni faqat shu paytda oʻqiydi), koʻrsatmangiz, profil maʼlumotlaringiz va tanlangan shablon serverimiz orqali OpenAI’ga (AQSH) yuboriladi va matnni uning modeli yozadi. «Yozish» tugmasini bosganingizda faqat koʻrsatmangiz va grammatik jinsingiz yuboriladi; nusxalangan xabar faqat «Nusxalanganga javob berish»ni tanlasangiz, javob sifatida yuboriladi. Aqlli tuzatish yoqilgan boʻlsa, AI Reply’ning oʻz maydonida yozayotgan koʻrsatmangiz tuzatish taklif qilish uchun yozish paytida ham yuboriladi.",
					"Serverimiz bu matnlarni faqat soʻrovni qayta ishlash vaqtida xotirada saqlaydi va ularni na bazaga, na jurnalga yozadi. OpenAI’ga ular saqlash oʻchirilgan API orqali (store=false) yuboriladi; OpenAI ularni oʻzining API qoidalari boʻyicha qayta ishlaydi: masalan, suiisteʼmolni aniqlash uchun cheklangan muddat saqlashi mumkin va sukut boʻyicha API maʼlumotlarini modellarni oʻqitishda ishlatmaydi.",
					"Bu faqat rozilik ekranida rozilik berganingizdan keyin sodir boʻladi. Rozilikni istalgan vaqtda Sozlamalar → Maxfiylik boʻlimida qaytarib olish mumkin.",
					"Android’da koʻrsatmani ovoz bilan aytish mumkin. Nutq imkon boʻlsa telefonning oʻzida, aks holda telefonning nutqni tanish xizmati (odatda Google) tomonidan aniqlanadi; AI Reply faqat aniqlangan matnni oladi, ovoz yozuvini emas.",
					"iPhone ilovasida javob beriladigan xabarni, profilni yoki shablonni ovoz bilan aytish mumkin. Nutq imkon boʻlsa iPhone’ning oʻzida Apple nutqni tanish tizimi, aks holda Apple nutqni tanish xizmati tomonidan aniqlanadi; AI Reply faqat aniqlangan matnni oladi, ovoz yozuvini emas.",
				}, "\n")},
				{"Nimalarni yigʻmaymiz", "Biz kontaktlar, geolokatsiya va reklama identifikatorlarini yigʻmaymiz hamda sizni boshqa ilovalarda kuzatmaymiz. Boshqa ilovalarda yozganlaringizdan hech narsa yuborilmaydi, faqat «Javob berish»ni bosganingizda nusxalangan yoki oʻzingiz belgilagan xabar va AI Reply’ning oʻz maydonidagi koʻrsatmangiz yuboriladi. Almashish buferi va belgilangan matn faqat ulardan foydalanadigan amalni bosganingizda oʻqiladi, masalan «Javob berish», «Nusxalanganga javob berish», shablon yoki qoʻyish tugmasi. Parollar va himoyalangan maydonlar hech qachon qayta ishlanmaydi."},
				{"Maʼlumotlarni biz uchun kim qayta ishlaydi", "OpenAI — javoblar, xabarlar va tuzatishlarni yozadi. Google — Google orqali kirish, Firebase Cloud Messaging (ikkala platformadagi bildirishnomalar) va Android’da, agar ishlatilsa, telefonning nutqni tanish xizmati. Apple — Apple orqali kirish, iPhone’ga bildirishnomalarni yetkazish (Firebase orqali Apple Push Notification service) va iPhone ilovasida ovoz bilan aytsangiz, Apple nutqni tanish tizimi. Resend — kirish kodlari va hisob xatlarini yuboradi. Serverimizning xosting provayderi — maʼlumotlar bazasini saqlaydi. Ularning har biri faqat oʻz vazifasi uchun keraklisini oladi."},
				{"Maʼlumotlarni boshqa davlatlarga uzatish", "OpenAI, Google, Apple va Resend maʼlumotlarni AQSH va boshqa davlatlarda qayta ishlaydi. Shu sababli ularga uzatilgan maʼlumotlar mamlakatingizdan tashqariga chiqishi mumkin; ular maʼlumotlarni oʻzlarining maʼlumotlarni himoya qilish va xavfsizlik majburiyatlariga muvofiq qayta ishlaydi."},
				{"Maʼlumotlar qancha saqlanadi", "Hisob maʼlumotlari — pochta va kirish usullari, profil va sozlamalar, rozilik yozuvlari, qurilmalar, hisoblagichlar, shikoyatlar, bildirishnoma sozlamalari, shuningdek shaxsan sizga yuborilgan bildirishnomaning qabul qiluvchilar roʻyxatidagi pochtangiz yoki hisob raqamingiz — hisob oʻchirilgunga qadar saqlanadi. Boshqa maʼlumotlar avtomatik oʻchiriladi: " + strings.Join([]string{
					"soʻrov metamaʼlumotlari — " + p(r.AIUsageEventsDays),
					"ilova hodisalari — " + p(r.ProductEventsDays),
					"kirish va oʻchirish kodlari (berilgan paytdan) — " + p(r.OTPDays),
					"tugagan seanslar (muddati tugagan yoki chiqilgan paytdan) — " + p(r.RefreshTokenDays),
					"hisobga bogʻlanmagan ilova oʻrnatilishi (oxirgi marta koʻringan paytdan) — " + p(r.AnonInstallationsDays),
					notices("tugagan bildirishnomalar va yetkazishlar", "tugagan bildirishnoma yetkazishlari", "bildirishnomalarning oʻzi"),
				}, "; ") + ". Administratorlarning hisob bilan bogʻliq harakatlari jurnali (masalan, bloklash yoki tarifni oʻzgartirish) hisobning tasodifiy raqamini pochtangizsiz saqlaydi va avtomatik oʻchirilmaydi, hisob oʻchirilgandan keyin ham. Nusxalangan xabarlar, koʻrsatmalar va yaratilgan javoblar umuman saqlanmaydi, faqat oʻzingiz yuborgan shikoyatdagi javob matni bundan mustasno."},
				{"Kirish va xavfsizlik", "Admin panelga faqat vakolatli administratorlar kira oladi. Unda pochtasining bir qismi yashirilgan hisoblar, tariflar, hisoblagichlar, soʻrov metamaʼlumotlari va yuborilgan shikoyatlar koʻrinadi; soʻrov matnlari ekrani yoʻq, chunki ular saqlanmaydi. Ulanishlar shifrlanadi, push-tokenlar shifrlangan holda, kirish kodlari esa faqat xesh koʻrinishida saqlanadi."},
				{"Tanlovingiz va huquqlaringiz", "Ilovada profilingizni koʻrish va oʻzgartirish, bildirishnoma toifalarini yoqish yoki oʻchirish (reklama bildirishnomalari oʻzingiz yoqmaguningizcha oʻchiq) va Sozlamalar → Maxfiylik boʻlimida AI ishlov berishga rozilikni qaytarib olish mumkin: yana rozilik bermaguningizcha AI javoblar ishlamaydi, hisob saqlanadi. Hisobni Sozlamalar → Hisob → Hisobni oʻchirish boʻlimida yoki " + info.DeletionURL + " sahifasida oʻchirish mumkin — u darhol oʻchiriladi. " + apple(
					"Apple orqali kirgan boʻlsangiz, iPhone ilovasida hisobni oʻchirganingizda Apple’dan Apple orqali kirishni uzishni ham soʻraymiz; AI Reply’ni Apple ID sozlamalaridagi «Apple orqali kirish» boʻlimidan istalgan vaqtda oʻzingiz ham olib tashlashingiz mumkin.",
					"Apple orqali kirgan boʻlsangiz, AI Reply’ni Apple ID sozlamalaridagi «Apple orqali kirish» boʻlimidan olib tashlashingiz mumkin.") + " Maʼlumotlaringiz nusxasini olish, ularni tuzatish yoki savol berish uchun " + reach + "."},
				{"Bolalar", "AI Reply 13 yoshga toʻlmagan bolalar uchun moʻljallanmagan."},
				{"Siyosatdagi oʻzgarishlar", "Siyosatning yangi tahriri ushbu sahifada sanasi bilan eʼlon qilinadi. Agar oʻzgarish matnlarni qayta ishlashga taalluqli boʻlsa, AI javoblar yana ishlashi uchun ilova yangi versiyani qabul qilishni soʻraydi."},
			}
		}
	}
	return renderSections(sections, locale)
}

// renderSections — тақырып пен мәтін; мәтіндегі әр жол — бөлек абзац.
func renderSections(all map[string][][2]string, locale string) string {
	sections, ok := all[locale]
	if !ok {
		sections = all["en"]
	}
	var b strings.Builder
	for _, s := range sections {
		b.WriteString("<h2>")
		b.WriteString(template.HTMLEscapeString(s[0]))
		b.WriteString("</h2>")
		for _, paragraph := range strings.Split(s[1], "\n") {
			b.WriteString("<p>")
			b.WriteString(template.HTMLEscapeString(paragraph))
			b.WriteString("</p>")
		}
	}
	return b.String()
}
