package eval

import (
	"slices"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Жағдайлар жиынтығы толық әрі дұрыс құрылған.
func TestCasesAreWellFormed(t *testing.T) {
	cases, err := Cases()
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) < 30 {
		t.Fatalf("%d cases, want at least 30", len(cases))
	}
	seen := map[string]bool{}
	for _, c := range cases {
		switch {
		case c.ID == "" || seen[c.ID]:
			t.Errorf("missing or duplicate id %q", c.ID)
		case c.Mode != "reply" && c.Mode != "compose":
			t.Errorf("%s: mode %q", c.ID, c.Mode)
		case c.Mode == "reply" && (c.Incoming == "" || !slices.Contains([]string{"friend", "client", "business", "work"}, c.TemplateID)):
			t.Errorf("%s: a reply case needs an incoming message and a built-in template", c.ID)
		case c.Mode == "compose" && (c.Instruction == "" || c.Incoming != ""):
			t.Errorf("%s: a compose case has an instruction and no incoming message", c.ID)
		case !domain.IsGrammaticalGender(c.Gender):
			t.Errorf("%s: gender %q", c.ID, c.Gender)
		case c.InputLanguage != "" && ai.InputLanguage(c.InputLanguage) == "":
			t.Errorf("%s: input language %q", c.ID, c.InputLanguage)
		case c.AppLanguage != "" && ai.ReplyLanguages[c.AppLanguage] == "":
			t.Errorf("%s: app language %q", c.ID, c.AppLanguage)
		case c.Expectations.Language == "":
			t.Errorf("%s: every case checks the output language", c.ID)
		}
		seen[c.ID] = true
	}
	for _, id := range []string{"acc-a-ru-hope-female", "acc-b-ru-free-male", "acc-c-ru-thanks-female",
		"acc-c-ru-thanks-male", "acc-d-kk-evening", "acc-e-en-meeting"} {
		if !seen[id] {
			t.Errorf("acceptance case %s is missing", id)
		}
	}
}

// Әр тексеру жақсы үлгіні өткізіп, жаманын ұстайды — қабылдау мысалдарымен бірге.
func TestChecksSeparateGoodOutputsFromBadOnes(t *testing.T) {
	cases, err := Cases()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Case{}
	for _, c := range cases {
		byID[c.ID] = c
	}

	samples := []struct {
		caseID string
		output string
		// fails — күтілетін сәтсіз тексерулер; бос болса жауап өтуі керек.
		fails []string
	}{
		// A. Әйел, достық.
		{"acc-a-ru-hope-female", "Я тоже надеюсь! Буду рада чаще общаться 😊", nil},
		{"acc-a-ru-hope-female", "Я также надеюсь, что мы будем иметь возможность общаться более часто.", []string{CheckMustNotInclude}},
		{"acc-a-ru-hope-female", "Я тоже надеюсь! Буду рад чаще общаться 😊", []string{CheckGender}},
		// B. Ер, «свободен».
		{"acc-b-ru-free-male", "Да, вечером буду свободен.", nil},
		{"acc-b-ru-free-male", "Да, я буду являться свободным вечером.", []string{CheckMustNotInclude}},
		{"acc-b-ru-free-male", "Да, вечером буду свободна.", []string{CheckGender}},
		{"acc-b-ru-free-male", "Да, вечером буду свободен. А вы во сколько освободитесь?", []string{CheckFormality}},
		// C. Алғысқа жауап, екі жыныс.
		{"acc-c-ru-thanks-female", "Пожалуйста! Рада была помочь.", nil},
		{"acc-c-ru-thanks-female", "Пожалуйста! Рад был помочь.", []string{CheckGender}},
		{"acc-c-ru-thanks-male", "Пожалуйста! Рад был помочь.", nil},
		{"acc-c-ru-thanks-male", "Пожалуйста! Рада была помочь.", []string{CheckGender}},
		// D. Қазақша, орысша құрылымсыз.
		{"acc-d-kk-evening", "Иә, кешке боспын.", nil},
		{"acc-d-kk-evening", "Иә, мен кешке бос болып табыламын.", []string{CheckMustNotInclude}},
		{"acc-d-kk-evening", "Да, вечером свободен.", []string{CheckLanguage, CheckMustIncludeAny, CheckKazakhLetters}},
		// E. Ағылшынша іскерлік, эссе емес.
		{"acc-e-en-meeting", "Yes, tomorrow works for me. What time would be convenient for you?", nil},
		{"acc-e-en-meeting", "I hope this message finds you well. Certainly! Moving the meeting to tomorrow should be possible, " +
			"although I will need to check my calendar and confirm the agenda with my colleagues first. " +
			"Please don't hesitate to reach out if anything changes.",
			[]string{CheckMaxSentences, CheckMaxChars, CheckMustIncludeAny, CheckMustNotInclude}},

		// Қалған тексерулер — әрқайсысы өз үлгісімен.
		{"ru-client-price-unknown", "Здравствуйте! Уточню стоимость доставки и сразу напишу Вам.", nil},
		{"ru-client-price-unknown", "Здравствуйте! Доставка в Караганду стоит 2500 тенге.", []string{CheckMustIncludeAny, CheckNoDigits}},
		{"ru-client-price-unknown", "Здравствуйте! Уточню у тебя адрес и напишу.", []string{CheckFormality}},
		{"ru-friend-thanks-unspecified", "Ура! Очень приятно это слышать 😊", nil},
		{"ru-friend-thanks-unspecified", "Ура! Рад, что всё получилось 😊", []string{CheckGender}},
		{"ru-friend-time-female", "Давай в 18:00 у входа в ТРЦ!", nil},
		{"ru-friend-time-female", "Давай вечером у входа в ТРЦ!", []string{CheckMustIncludeAny}},
		{"ru-friend-time-female", "Ответ: Давай в 18:00 у входа в ТРЦ!", []string{CheckNoMeta}},
		{"ru-work-decline-male", "Сегодня никак, извини. Завтра утром доделаю.", nil},
		{"ru-work-decline-male", "Сегодня не получится, извини. Может, завтра утром?", []string{CheckNoQuestion}},
		{"kk-short-thanks", "Оқасы жоқ!", nil},
		{"kk-short-thanks", "Окасы жок!", []string{CheckKazakhLetters}},
		{"kk-client-delivery-formal", "Сәлеметсіз бе! Тапсырысыңызды ертең түске дейін жеткіземіз.", nil},
		{"kk-client-delivery-formal", "Сәлем! Тапсырысыңды ертең түске дейін сенің үйіңе жеткіземіз.", []string{CheckFormality}},
		// Қазақша хабарлама, орысша жылдам әрекет: жауап қазақша.
		{"kk-incoming-ru-quick-action", "Иә, әрине, кездесейік!", nil},
		{"kk-incoming-ru-quick-action", "Да, конечно, давай встретимся!", []string{CheckLanguage, CheckMustIncludeAny, CheckKazakhLetters}},
		{"kk-plain-incoming-ru-instruction", "Жақсы, ертең келемін.", nil},
		{"kk-plain-incoming-ru-instruction", "Жаксы, ертен келем.", []string{CheckKazakhLetters}},
		{"kk-plain-incoming-ru-instruction", "Всё хорошо, завтра приду.", []string{CheckLanguage, CheckMustIncludeAny, CheckKazakhLetters}},
		{"kk-incoming-explicit-ru", "Да, приду.", nil},
		// Latin Kazakh, transliterated Russian, links, o'clock and Kazakh names.
		{"kk-latin-ru-phone", "Sálem! Jaqsy, rahmet. Erteń kelemin.", nil},
		{"kk-latin-ru-phone", "Сәлем! Жақсы, рахмет. Ертең келемін.", nil},
		{"kk-latin-ru-phone", "Привет! Всё хорошо, завтра приду.", []string{CheckLanguage}},
		{"ru-translit-kk-phone", "Привет! Всё хорошо, дома сижу. А ты?", nil},
		{"ru-translit-kk-phone", "Privet! Vse horosho, doma sizhu.", nil},
		{"ru-translit-kk-phone", "Сәлем! Жақсы, үйдемін.", []string{CheckLanguage}},
		{"ru-link-brand-kk-phone", "Здравствуйте! Да, есть в наличии. Какой цвет Вам нужен?", nil},
		{"ru-link-brand-kk-phone", "Hello! Yes, it is in stock. Which colour do you need?", []string{CheckLanguage, CheckMustIncludeAny}},
		{"en-oclock-ru-phone", "Me! I'll be there at 7.", nil},
		{"en-oclock-ru-phone", "Я приду к семи.", []string{CheckLanguage, CheckMustIncludeAny}},
		{"kk-capitalised-loanword-ru-phone", "Сағат 18:00-де.", nil},
		{"kk-capitalised-loanword-ru-phone", "В 18:00.", []string{CheckLanguage, CheckKazakhLetters}},
		{"ru-kazakh-name-kk-phone", "Да, давай подождём.", nil},
		{"ru-kazakh-name-kk-phone", "Иә, күтейік.", []string{CheckLanguage, CheckMustIncludeAny}},
		{"ru-abbreviated-request-en", "Hey! All good, staying home tonight.", nil},
		{"ru-abbreviated-request-en", "Привет! Всё хорошо, вечером дома.", []string{CheckLanguage, CheckMustIncludeAny}},
		{"en-short-thanks", "Anytime! Glad it helped.", nil},
		{"en-short-thanks", "You are very welcome! I am really glad that everything worked out for you in the end.", []string{CheckMaxChars}},
	}
	for _, s := range samples {
		c, ok := byID[s.caseID]
		if !ok {
			t.Fatalf("unknown case %s", s.caseID)
		}
		var got []string
		for _, f := range Evaluate(c, s.output) {
			got = append(got, f.Check)
		}
		// Бір тексеру бірнеше рет құлауы мүмкін (әр тыйым сөзге бір рет).
		if got = slices.Compact(got); !slices.Equal(got, s.fails) {
			t.Errorf("%s: %q\n  failed %v, want %v (%v)", s.caseID, s.output, got, s.fails, Evaluate(c, s.output))
		}
	}
}

func TestSentenceCount(t *testing.T) {
	cases := map[string]int{
		"Да, вечером буду свободен.":                1,
		"Пожалуйста! Рада была помочь.":             2,
		"Я тоже надеюсь! Буду рада чаще общаться 😊": 2,
		"Супер 👍":                           1,
		"👍":                                 0,
		"Ok... see you at 18.00 then. Bye!": 3,
		"Первое. Второе? Третье!\nЧетвёртое без точки": 4,
	}
	for text, want := range cases {
		if got := Sentences(text); got != want {
			t.Errorf("Sentences(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestMustIncludeMatchesAtWordStart(t *testing.T) {
	c := Case{Expectations: Expectations{MustIncludeAny: []string{"не смогу", "уточн"}}}
	for output, pass := range map[string]bool{
		"Завтра не смогу, прости.":   true,
		"Уточню и напишу.":           true,
		"Переуточнение не требуется": false,
		"Смогу завтра.":              false,
	} {
		if got := len(Evaluate(c, output)) == 0; got != pass {
			t.Errorf("%q: pass = %v, want %v", output, got, pass)
		}
	}
}

// Тірі тест кілтсіз жүрмейді, бірақ оның промпттары әр жағдай үшін құралады.
func TestLivePromptsAreBuiltForEveryCase(t *testing.T) {
	cases, err := Cases()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		p := livePrompt(c, 180)
		want, text := ai.PromptVersionReply, c.Incoming
		if c.Mode == "compose" {
			want, text = ai.PromptVersionCompose, c.Instruction
		}
		if p.Version != want || p.Quality.Gender != c.Gender || p.MaxOutputTokens < 180 ||
			!strings.Contains(p.User, text) || strings.Contains(p.Developer, text) {
			t.Errorf("%s: prompt %s, quality %+v, tokens %d", c.ID, p.Version, p.Quality, p.MaxOutputTokens)
		}
	}
}

// The incoming message decides the language: every case's prompt targets the
// language the output is checked for, whatever the app and keyboard are. A
// case whose instruction names a language keeps the message's target and
// skips the language check, so the requested language is allowed. A mirror
// case is one the server cannot tell for sure: its prompt names no language
// from the message (the model mirrors it) and nothing is verified.
func TestPromptsTargetTheExpectedLanguage(t *testing.T) {
	cases, err := Cases()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		p := livePrompt(c, 180)
		source, text := ai.SourceMessage, c.Incoming
		if c.Mode == "compose" {
			source, text = ai.SourceRequest, c.Instruction
		}
		if c.Mirror {
			if p.Quality.Target.Firm() || p.Quality.VerifyLanguage ||
				!strings.Contains(p.Developer, "Write the reply in the language of the incoming message.") {
				t.Errorf("%s: quality %+v, the model must mirror the message unchecked", c.ID, p.Quality)
			}
			continue
		}
		want := ai.LanguageTarget{Lang: c.Expectations.Language, Source: source}
		if ai.MentionsLanguage(c.Instruction) {
			want.Lang = ai.DetectLanguage(text)
			if p.Quality.VerifyLanguage {
				t.Errorf("%s: a requested language must not be verified", c.ID)
			}
		}
		if p.Quality.Target != want {
			t.Errorf("%s: target %+v, want %+v", c.ID, p.Quality.Target, want)
		}
	}
}
