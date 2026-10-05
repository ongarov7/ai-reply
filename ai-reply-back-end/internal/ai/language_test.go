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
		"reply in English", "орысша жауап бер", "o'zbekcha yoz", "ответь на русском", "Казакша жауап бер"} {
		if !MentionsLanguage(text) {
			t.Errorf("%q names a language", text)
		}
	}
	for _, text := range []string{"Скажи что вечером свободен", "Кешке бос екенімді айт", "Ответь согласием.",
		"Скажи, что доставка по Казахстану бесплатная", "Қазақстан бойынша жеткізу тегін",
		"(If I named a language above, write the reply in that language.)", "Мы летим в Англию"} {
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
