package ai

import "testing"

func TestDetectLanguage(t *testing.T) {
	cases := []struct {
		text      string
		lang      string
		confident bool
	}{
		// Орыс тілі: белгі сөздер, жиілік тізімі, орыс жалғаулары.
		{"Привет! Давно не общались. Как ты?", "ru", true},
		{"Надеюсь, теперь будем чаще общаться", "ru", true},
		{"Ты завтра свободен?", "ru", true},
		{"Получится сегодня созвониться?", "ru", true},
		{"Встреча переносится на четверг", "ru", true},
		{"Не могу сейчас говорить", "ru", true},
		{"Понятно", "ru", true},
		{"Ок, понял", "ru", true},
		// Қазақ тілі арнайы әріптермен — қысқа да, ұзын да.
		{"Ертең кездесуге уақытың бар ма?", "kk", true},
		{"Сәлем", "kk", true},
		{"Иә, болады", "kk", true},
		{"Рақмет!", "kk", true},
		{"Түсіндім", "kk", true},
		{"Ертең Алматыға барамын", "kk", true},
		// Қазақ тілі арнайы әріптерсіз (орыс пернетақтасында) — қысқа да.
		{"Калайсын?", "kk", true},
		{"Кайдасын?", "kk", true},
		{"Рахмет!", "kk", true},
		{"Иа", "kk", true},
		{"Калайсын? Бугин келесин бе?", "kk", true},
		{"Мен ертен келем", "kk", true},
		{"Ертен жумыска барасын ба?", "kk", true},
		{"Бугин келмеймин, ауырып калдым", "kk", true},
		{"Не болды?", "kk", true},
		{"Ок, рахмет", "kk", true},
		// Ескі мәтіндердегі латын i — қазақтың і әрпі.
		{"бiз ертең барамыз", "kk", true},
		// Аралас: қазақ сөздері орыс дәлелінің кемінде ¾ бөлігі болса — қазақша.
		{"Сәлем, как дела?", "kk", true},
		{"Сәлем, как дела? Ертең келесің бе?", "kk", true},
		{"Привет, қалайсың?", "kk", true},
		{"Түсіндім, спасибо", "kk", true},
		{"Слушай, ертең жиналыс бар ма?", "kk", true},
		{"Сәлем! Ертең қалай, кездесеміз бе? Ок", "kk", true},
		// Орыс тілі басым болса — орысша.
		{"Сәлем! Как дела? Сегодня придёшь? Давно не виделись", "ru", true},
		// Қазақ есімі орысша хабарды қазақша етпейді.
		{"Әсел, ты где?", "ru", true},
		{"Айгүл придёт завтра?", "ru", true},
		{"Нұрлан, перезвони мне", "ru", true},
		{"Нурлан, перезвони мне", "ru", true},
		{"Айгерим, ты завтра придёшь?", "ru", true},
		// Ағылшын және өзбек латыны.
		{"Can we move the meeting to tomorrow?", "en", true},
		{"Thanks a lot!", "en", true},
		{"Salom! Ertaga uchrashamizmi?", "uz", true},
		{"Bugun ishdaman, oʻzim yozaman", "uz", true},
		{"Qachon kelasan?", "uz", true},
		// Анық емес: аралас әріпсіз, жалғыз есім, бір қысқа сөз.
		{"Калайсын, как дела?", "ru", false},
		{"Айгүл!", "kk", false},
		{"Да", "ru", false},
		{"Hi", "en", false},
		// Дәлел жоқ.
		{"👍", "", false},
		{"18:00", "", false},
		{"ok", "", false},
		{"Ок", "", false},
		{"Хаха", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got := Detect(c.text)
		if got.Lang != c.lang || got.Confident != c.confident {
			t.Errorf("Detect(%q) = %+v, want {%s %v} (evidence %+v)", c.text, got, c.lang, c.confident, gatherEvidence(c.text))
		}
		if DetectLanguage(c.text) != c.lang {
			t.Errorf("DetectLanguage(%q) = %q, want %q", c.text, DetectLanguage(c.text), c.lang)
		}
	}
}

// detectionCase — want is the expected best guess; confident whether the
// text alone may decide the reply language.
type detectionCase struct {
	text      string
	lang      string
	confident bool
}

func checkDetections(t *testing.T, cases []detectionCase) {
	t.Helper()
	for _, c := range cases {
		if got := Detect(c.text); got.Lang != c.lang || got.Confident != c.confident {
			t.Errorf("Detect(%q) = %+v, want {%s %v} (evidence %+v)", c.text, got, c.lang, c.confident, gatherEvidence(c.text))
		}
	}
}

// notConfidently — the text must never decide the reply language as lang.
func notConfidently(t *testing.T, lang string, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if got := Detect(text); got.Confident && got.Lang == lang {
			t.Errorf("Detect(%q) = %+v: confidently %s (evidence %+v)", text, got, lang, gatherEvidence(text))
		}
	}
}

// Review: Kazakh in Latin letters came out as confident Uzbek and Russian in
// Latin letters as confident English, which forced a wrong-language reply.
func TestDetectLatinKazakhAndTransliteratedRussian(t *testing.T) {
	checkDetections(t, []detectionCase{
		{"Salem! Qalaisyn?", "kk", true},
		{"Qashan kelesin?", "kk", true},
		{"Jaqsy, rahmet!", "kk", true},
		{"Men keshke kelemin", "kk", true},
		{"Sálem! Qalaısyń?", "kk", true},
		{"Erten kelesin be?", "kk", true},
		{"Salem kalaisyn kaidasyn", "kk", true},
		{"Qaida sin?", "kk", true},
		{"Rahmet, jaqsy", "kk", true},
		{"Men keldim", "kk", true},
		// Russian in Latin letters: a best guess only, the model mirrors it.
		{"Privet, kak dela?", "ru", false},
		{"Napishi chto ya opozdayu", "ru", false},
		{"Privet kak dela chto delaesh", "ru", false},
		{"Privet! Ty gde?", "ru", false},
		{"Zdravstvuyte, skolko stoit?", "ru", false},
		// Unknown Latin words and a bare q never decide.
		{"Uide bar ma?", "kk", false},
		{"Qyzyq", "kk", false},
		// Uzbek and English keep their real evidence.
		{"Qachon kelasan?", "uz", true},
		{"Salom! Ertaga uchrashamizmi?", "uz", true},
		{"Thanks a lot!", "en", true},
		{"Happy birthday!", "en", true},
	})
	notConfidently(t, "uz", "Qazir kele almaımyn", "Sälem! Qalaisyñ?", "Qalaysyz", "Sen qaidasyn?")
	notConfidently(t, "en", "Privet, kak dela?", "Napishi chto ya opozdayu", "Erten kelesin be?", "Uide bar ma?")
}

// Review: "Who's", "o'clock" and "Greg's" were read as Uzbek oʻ/gʻ.
func TestDetectEnglishContractionsAreNotUzbek(t *testing.T) {
	checkDetections(t, []detectionCase{
		{"Who's coming?", "en", true},
		{"Who’s free?", "en", true},
		{"Who's in?", "en", true},
		{"At 5 o'clock?", "en", true},
		{"Who's calling?", "en", true},
		{"Who's coming to the party tonight?", "en", true},
		{"Bugun ishdaman, oʻzim yozaman", "uz", true},
		{"O'zbekiston go'zal", "uz", true},
	})
	notConfidently(t, "uz", "Greg's here", "Who's there", "Doug's here", "Go's fine", "Bob's car", "O'Brien deemed Watt")
}

// Review: a Kazakh name in a Russian message (lowercase, or first in the
// sentence, or with an ending that looks Kazakh) made the message
// confidently Kazakh, and the repair translated a correct Russian reply.
func TestDetectKazakhNamesInRussianMessages(t *testing.T) {
	notConfidently(t, "kk",
		"әсем сказала что опоздает", "передай гүлназ что я позвоню", "әсел, ты где?", "айгүл придёт завтра?",
		"нұрлан, перезвони мне", "ты видел нұрлана?", "Мұхтар, ты где?", "Тоқтар, перезвони", "Мәди, ты придёшь?",
		"Оңдасын, привет", "Ты в Қарағанды?", "әсел и нұрлан придут?")
	checkDetections(t, []detectionCase{
		{"әсем сказала что опоздает", "ru", true},
		{"әсел, ты где?", "ru", true},
		{"Мұхтар, ты где?", "ru", true},
		{"Ты в Қарағанды?", "ru", true},
	})
}

// Review: a capitalised first word was taken for a name, so Kazakh with a
// Russian loanword became confidently Russian.
func TestDetectSentenceInitialKazakhWords(t *testing.T) {
	checkDetections(t, []detectionCase{
		{"Сағат нешеде встреча?", "kk", true},
		{"Үйге кел, срочно", "kk", true},
		{"Қанша стоит?", "kk", true},
		{"Керемет, спасибо!", "kk", true},
		{"Тамаша, спасибо!", "kk", true},
		{"Нет, бармаймын", "kk", true},
		{"Не истеп жатырсын?", "kk", true},
	})
}

// Review: links and brand names counted as English.
func TestDetectIgnoresLinksHandlesAndBrandNames(t *testing.T) {
	checkDetections(t, []detectionCase{
		{"iPhone 15 Pro Max есть?", "ru", true},
		{"Здравствуйте, сколько стоит Samsung Galaxy S24 Ultra?", "ru", true},
		{"Смотри какое видео https://www.youtube.com/watch?v=dQw4w9WgXcQ&feature=share", "ru", true},
		{"Посмотри https://www.instagram.com/reel/C8xyzAB12/?igsh=MWQ1ZGUxMzBkMA==", "ru", true},
		{"Вот ссылка https://docs.google.com/document/d/x/edit?usp=sharing", "ru", true},
		{"Напиши мне на почту aigerim@example.com или в телеграм @aigerim_k", "ru", true},
		{"Здравствуйте! iPhone 15 Pro Max есть в наличии? Вот ссылка https://kaspi.kz/shop/p/apple-iphone-15-pro-max", "ru", true},
		{"iPhone 15 Pro Max бар ма?", "kk", true},
		{"Apple Watch Series 9 бар ма?", "kk", true},
		{"Мына видеоны көрші https://www.youtube.com/watch?v=x", "kk", true},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", "", false},
		{"#акция @shop_kz 2500", "", false},
	})
}

// Uzbek Cyrillic shares қ and ғ with Kazakh; ў, ҳ and Uzbek words decide.
func TestDetectUzbekCyrillic(t *testing.T) {
	checkDetections(t, []detectionCase{
		{"Ассалому алайкум! Қалайсиз? Ишлар яхшими?", "uz", true},
		{"Қаердасиз?", "uz", true},
		{"Раҳмат, яхши", "uz", true},
	})
}

// Екі тізімде де бар сөз әлдеқайда жиі кездесетін тілге жатады.
func TestFrequencyListsDecideSharedWords(t *testing.T) {
	cases := []struct {
		word  string
		lang  string
		found bool
	}{
		{"как", "ru", true},
		{"бар", "kk", true},
		{"калайсын", "kk", true}, // the plain spelling of «қалайсың»
		{"понятно", "ru", true},
		{"zzzz", "", false},
	}
	for _, c := range cases {
		if lang, found := cyrillicListLanguage(c.word); lang != c.lang || found != c.found {
			t.Errorf("cyrillicListLanguage(%q) = %q %v, want %q %v", c.word, lang, found, c.lang, c.found)
		}
	}
	if kazakhRanks["қалайсың"] == 0 || kazakhRanks["қалайсың"] != kazakhRanks["калайсын"] {
		t.Fatalf("a plain spelling shares the rank of its original: %d %d", kazakhRanks["қалайсың"], kazakhRanks["калайсын"])
	}
	if len(russianRanks) < 5000 || len(kazakhRanks) < 5000 || len(englishRanks) < 5000 {
		t.Fatalf("frequency lists not loaded: ru %d, kk %d, en %d", len(russianRanks), len(kazakhRanks), len(englishRanks))
	}
}

func TestFrequencyRanks(t *testing.T) {
	ranks := frequencyRanks("# comment\nбұл\nбул\nжәне\nжане\nмен\n\nЁлка\n", kazakhPlain)
	want := map[string]int{"бұл": 1, "бул": 1, "және": 2, "жане": 2, "мен": 3, "елка": 4}
	if len(ranks) != len(want) {
		t.Fatalf("ranks = %v", ranks)
	}
	for word, rank := range want {
		if ranks[word] != rank {
			t.Errorf("rank of %q = %d, want %d", word, ranks[word], rank)
		}
	}
}

func TestMentionsLanguage(t *testing.T) {
	for _, text := range []string{"Ответь на казахском", "қазақша жаз", "Напиши по-английски",
		"reply in English", "орысша жауап бер", "o'zbekcha yoz", "ответь на русском", "Казакша жауап бер",
		// Review: abbreviations and Latin or colloquial forms.
		"ответь на англ", "по англ ответь", "напиши по-англ", "ответь на каз", "на англ.", "на инглише",
		"ответь на инглиш", "in eng pls", "reply in eng", "qazaqsha jaz", "kazaksha jaz", "otvet' na kazahskom",
		"на рус", "на каз", "rus tilida javob ber", "in Qazaq", "англше жаз", "Напиши поздравление на каз",
		"ответь по-русски", "orysha jaz", "in ru", "на kz", "ағылшынша жаз", "агылшынша", "inglizcha yoz",
		"oʻzbekcha", "ozbekcha yoz", "на узбекском", "өзбекше жаз", "ответь на engl", "по-казахски"} {
		if !MentionsLanguage(text) {
			t.Errorf("%q names a language", text)
		}
	}
	for _, text := range []string{"Скажи что вечером свободен", "Кешке бос екенімді айт", "Ответь согласием.",
		"Скажи, что доставка по Казахстану бесплатная", "Қазақстан бойынша жеткізу тегін",
		"(If I named a language above, write the reply in that language.)", "Мы летим в Англию",
		"Пойдём в казино", "Мне казалось, что он прав", "Русло реки", "I code in Rust",
		"Скажи Казаковой, что я опоздаю", "Передай Казакову привет", "Орысбаев келеді деп айт",
		"Отправь ссылку kaspi.kz/shop"} {
		if MentionsLanguage(text) {
			t.Errorf("%q names no language", text)
		}
	}
}

func TestResolveReplyTarget(t *testing.T) {
	cases := []struct {
		name                            string
		preference, message, input, app string
		want                            LanguageTarget
	}{
		{"preference wins", "kk", "Привет, как дела?", "ru", "en", LanguageTarget{"kk", SourcePreference}},
		{"incoming message", "", "Привет, как дела?", "kk", "en", LanguageTarget{"ru", SourceMessage}},
		{"kazakh message on a russian phone", "", "Ертең кездесуге уақытың бар ма?", "ru", "ru", LanguageTarget{"kk", SourceMessage}},
		{"plain kazakh on a russian phone", "", "Калайсын? Ертен келесин бе?", "ru", "ru", LanguageTarget{"kk", SourceMessage}},
		{"short kazakh on a russian phone", "", "Кайдасын?", "ru", "ru", LanguageTarget{"kk", SourceMessage}},
		{"russian message on a kazakh phone", "", "Ты завтра свободен?", "kk", "kk", LanguageTarget{"ru", SourceMessage}},
		{"english message on a russian phone", "", "Can we move the meeting to tomorrow?", "ru", "ru", LanguageTarget{"en", SourceMessage}},
		{"latin kazakh on a russian phone", "", "Salem! Qalaisyn?", "ru", "ru", LanguageTarget{"kk", SourceMessage}},
		{"latin kazakh on a kazakh keyboard", "", "Qashan kelesin?", "kk", "ru", LanguageTarget{"kk", SourceMessage}},
		{"transliterated russian is not english", "", "Privet, kak dela?", "kk", "ru", LanguageTarget{"kk", SourceKeyboard}},
		{"russian with a product and a link", "", "iPhone 15 Pro Max есть? https://kaspi.kz/shop/p/1", "kk", "kk", LanguageTarget{"ru", SourceMessage}},
		{"russian with a kazakh name", "", "әсем сказала что опоздает", "kk", "kk", LanguageTarget{"ru", SourceMessage}},
		{"kazakh starting with a capitalised word", "", "Сағат нешеде встреча?", "ru", "ru", LanguageTarget{"kk", SourceMessage}},
		{"english with o'clock", "", "At 5 o'clock?", "kk", "ru", LanguageTarget{"en", SourceMessage}},
		{"keyboard when unclear", "", "👍", "kk", "en", LanguageTarget{"kk", SourceKeyboard}},
		{"app when no keyboard", "", "ok", "", "ru-KZ", LanguageTarget{"ru", SourceApp}},
		{"unknown codes are ignored", "auto", "ok", "de", "xx", LanguageTarget{}},
	}
	for _, c := range cases {
		got := ResolveReplyTarget(c.preference, c.message, c.input, c.app)
		if got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
	if (LanguageTarget{"kk", SourceKeyboard}).Firm() || !(LanguageTarget{"ru", SourceMessage}).Firm() {
		t.Fatal("only preference, message and request are firm")
	}
}

func TestResolveComposeTarget(t *testing.T) {
	if got := ResolveComposeTarget("Поздравь коллегу с днём рождения", "kk", "en"); got != (LanguageTarget{"ru", SourceRequest}) {
		t.Fatalf("request language: %+v", got)
	}
	if got := ResolveComposeTarget("Айгерим 🎉", "kk", "en"); got != (LanguageTarget{"kk", SourceKeyboard}) {
		t.Fatalf("keyboard fallback: %+v", got)
	}
	if got := ResolveComposeTarget("🎉", "", "uz"); got != (LanguageTarget{"uz", SourceApp}) {
		t.Fatalf("app fallback: %+v", got)
	}
}

func TestInputLanguage(t *testing.T) {
	for in, want := range map[string]string{"kk": "kk", " RU ": "ru", "en": "en", "uz": "", "kk-KZ": "", "": ""} {
		if got := InputLanguage(in); got != want {
			t.Errorf("InputLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}
