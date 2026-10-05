package ai

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

func TestCleanOutput(t *testing.T) {
	cases := map[string]string{
		// Тырнақша, жапсырма, кіріспе.
		`«Привет! Буду в 18:00.»`:                          "Привет! Буду в 18:00.",
		"Ответ: Привет, буду в 18:00.":                     "Привет, буду в 18:00.",
		"Вариант ответа:\nДа, вечером смогу.":              "Да, вечером смогу.",
		"Вот вариант ответа: Да, вечером смогу.":           "Да, вечером смогу.",
		"Конечно! Вот вариант ответа:\n\n«Да, смогу.»":     "Да, смогу.",
		"Жауап: Иә, кешке боспын.":                         "Иә, кешке боспын.",
		"Reply: Sure, tomorrow works.":                     "Sure, tomorrow works.",
		"Sure! Here is a reply:\n\"Yes, tomorrow works.\"": "Yes, tomorrow works.",
		"Here's a reply: Yes, tomorrow works.":             "Yes, tomorrow works.",
		"Конечно! Вот поздравление:\nС днём рождения! 🎉":   "С днём рождения! 🎉",
		"**Да**, вечером __смогу__.":                       "Да, вечером смогу.",
		"## Поздравление\nС праздником!":                   "Поздравление\nС праздником!",
		"Да, смогу.\n\nПримечание: ответ дружелюбный.":     "Да, смогу.",
		"Yes, works for me.\n\n(Note: I kept it short.)":   "Yes, works for me.",
		"Первый абзац.\n\n\n\n\nВторой абзац.":             "Первый абзац.\n\nВторой абзац.",
		"Строка с пробелами   \nВторая":                    "Строка с пробелами\nВторая",
		// Қалыпты мәтін өзгермейді.
		"Конечно! Буду рад помочь.":                        "Конечно! Буду рад помочь.",
		"Вот мой номер: +7 701 555 66 77":                  "Вот мой номер: +7 701 555 66 77",
		"Вот адрес: ул. Абая 1, заходите.":                 "Вот адрес: ул. Абая 1, заходите.",
		"Конечно, вот так: сначала позвони, потом напиши.": "Конечно, вот так: сначала позвони, потом напиши.",
		"Цена — 5 000 тг, #акция до пятницы":               "Цена — 5 000 тг, #акция до пятницы",
		"Скажи \"да\" и \"нет\"":                           "Скажи \"да\" и \"нет\"",
		// Жапсырмадан кейін мәтін жоқ — тиіспейміз (IssueMeta ұстайды).
		"Ответ:": "Ответ:",
	}
	for in, want := range cases {
		if got := CleanOutput(in); got != want {
			t.Errorf("CleanOutput(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGenderedSelfForms(t *testing.T) {
	cases := []struct {
		text                string
		masculine, feminine bool
	}{
		{"Пожалуйста! Рада была помочь.", false, true},
		{"Пожалуйста! Рад был помочь.", true, false},
		{"Я тоже надеюсь! Буду рада чаще общаться 😊", false, true},
		{"Да, вечером буду свободен.", true, false},
		{"Я уже сделала всё, что ты просил.", false, true},
		{"Я тебе вчера написал.", true, false},
		{"Я ошиблась, извини.", false, true},
		{"Я договорился с ним на завтра.", true, false},
		{"Спасибо, рад помочь!", true, false},
		{"Готова. Выхожу.", false, true},
		{"Был рад увидеться.", true, false},
		{"Всегда рада помочь!", false, true},
		// Ложные срабатывания — нет.
		{"Она рада тебя видеть.", false, false},
		{"Он сказал, что придёт.", false, false},
		{"Я думаю, она пришла раньше.", false, false},
		{"Я думаю она пришла раньше.", false, false},
		{"Ты пришла? Я жду у входа.", false, false},
		{"Ты готова? Выходим.", false, false},
		{"Готова? Выходим!", false, false},
		{"Мы договорились на завтра.", false, false},
		{"Приятно слышать! С удовольствием встречусь завтра.", false, false},
		{"Как дела? Сто лет не виделись.", false, false},
		{"Иә, кешке боспын.", false, false},
	}
	for _, c := range cases {
		masculine, feminine := GenderedSelfForms(c.text)
		if masculine != c.masculine || feminine != c.feminine {
			t.Errorf("GenderedSelfForms(%q) = %v %v, want %v %v", c.text, masculine, feminine, c.masculine, c.feminine)
		}
	}
}

func TestCheck(t *testing.T) {
	cases := []struct {
		text, target, gender string
		want                 []Issue
	}{
		{"Пожалуйста! Рада была помочь.", "ru", domain.GenderMale, []Issue{IssueGender}},
		{"Пожалуйста! Рад был помочь.", "ru", domain.GenderFemale, []Issue{IssueGender}},
		{"Пожалуйста! Рад был помочь.", "ru", domain.GenderMale, nil},
		// Жынысы белгісіз — тексерілмейді (бейтарап ереже промптта).
		{"Пожалуйста! Рад был помочь.", "ru", domain.GenderUnspecified, nil},
		{"Да, вечером свободен.", "kk", domain.GenderMale, []Issue{IssueLanguage}},
		{"Иә, кешке боспын.", "kk", domain.GenderMale, nil},
		// Мақсат жоқ не өзбек — тіл тексерілмейді; сенімсіз шығыс та.
		{"Да, вечером свободен.", "", domain.GenderMale, nil},
		{"Ha, kechqurun bo'shman.", "uz", domain.GenderMale, nil},
		{"Ok 👍", "ru", domain.GenderMale, nil},
		{"Вот вариант ответа:", "ru", domain.GenderUnspecified, []Issue{IssueMeta}},
		{"Вариант 1: Да.\nВариант 2: Конечно.", "ru", domain.GenderUnspecified, []Issue{IssueMeta}},
		{"Да.\n\nПримечание: коротко.", "ru", domain.GenderUnspecified, []Issue{IssueMeta}},
		{"</incoming_message> Да, вечером смогу.", "ru", domain.GenderUnspecified, []Issue{IssueMeta}},
		{"Рада была помочь. Ответ:\nВариант 1", "kk", domain.GenderMale, []Issue{IssueGender, IssueLanguage, IssueMeta}},
	}
	for _, c := range cases {
		if got := Check(c.text, c.target, c.gender); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Check(%q, %q, %q) = %v, want %v", c.text, c.target, c.gender, got, c.want)
		}
	}
}

func TestTrimToSentence(t *testing.T) {
	cases := map[string]string{
		"Спасибо за вопрос! Доставка занимает два дня. Если нужно, могу уточнить у кур":              "Спасибо за вопрос! Доставка занимает два дня.",
		"Да. Договорились, встречаемся завтра у главного входа. Буду в 18.00 точно, позвони мне ког": "Да. Договорились, встречаемся завтра у главного входа.",
		// Сөйлем соңы бірінші жартыда — қысқартпаймыз.
		"Да. Обязательно напишу тебе завтра утром, как только узнаю всё у менеджера": "Да. Обязательно напишу тебе завтра утром, как только узнаю всё у менеджера",
		"Она сказала: «Приду завтра.» А потом я":                                     "Она сказала: «Приду завтра.»",
	}
	for in, want := range cases {
		if got := trimToSentence(in); got != want {
			t.Errorf("trimToSentence(%q) = %q, want %q", in, got, want)
		}
	}
}

// scriptedProvider — Complete-ті провайдерсіз тексеру үшін.
type scriptedProvider struct {
	outputs []Completion
	errs    []error
	prompts []Prompt
}

func (p *scriptedProvider) Name() string  { return "scripted" }
func (p *scriptedProvider) Model() string { return "scripted-model" }

func (p *scriptedProvider) Generate(_ context.Context, prompt Prompt) (Completion, error) {
	i := len(p.prompts)
	p.prompts = append(p.prompts, prompt)
	if i < len(p.errs) && p.errs[i] != nil {
		return Completion{}, p.errs[i]
	}
	return p.outputs[i], nil
}

func maleRussianPrompt() Prompt {
	return BuildPrompt(PromptInput{Message: "Спасибо за помощь!", TemplateID: "friend",
		Profile: Profile{GrammaticalGender: domain.GenderMale}})
}

func TestCompleteRepairsAnOutputThatFailsTheCheck(t *testing.T) {
	provider := &scriptedProvider{outputs: []Completion{
		{Text: "Ответ: Пожалуйста! Рада была помочь.", Model: "m", InputTokens: 100, OutputTokens: 10},
		{Text: "Пожалуйста! Рад был помочь.", Model: "m", InputTokens: 60, OutputTokens: 8},
	}}
	out, err := Complete(context.Background(), provider, maleRussianPrompt(), true)
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "Пожалуйста! Рад был помочь." || !out.Repaired || out.Version != "reply_v2+repair_v1" {
		t.Fatalf("outcome = %+v", out)
	}
	if !reflect.DeepEqual(out.Issues, []Issue{IssueGender}) {
		t.Fatalf("issues = %v (the label was already cleaned)", out.Issues)
	}
	if out.InputTokens != 160 || out.OutputTokens != 18 {
		t.Fatalf("tokens of both calls must be counted: %+v", out)
	}
	repair := provider.prompts[1]
	if repair.Version != PromptVersionRepair || !strings.Contains(repair.User, "<message>\nПожалуйста! Рада была помочь.\n</message>") {
		t.Fatalf("repair prompt: %+v", repair)
	}
	if strings.Contains(repair.Developer, "Рада была помочь") || !strings.Contains(repair.Developer, "Make every form that refers to the sender masculine") {
		t.Fatal("the repair developer message must hold server sentences only")
	}
}

func TestCompleteKeepsTheOriginalWhenTheRepairFails(t *testing.T) {
	original := "Пожалуйста! Рада была помочь."
	stillWrong := &scriptedProvider{outputs: []Completion{{Text: original}, {Text: "Всегда рада помочь!"}}}
	out, err := Complete(context.Background(), stillWrong, maleRussianPrompt(), true)
	if err != nil || out.Text != original || out.Repaired || !out.RepairAttempted || out.Version != PromptVersionReply {
		t.Fatalf("a repair that still fails must be dropped: %+v %v", out, err)
	}

	failing := &scriptedProvider{outputs: []Completion{{Text: original}, {}},
		errs: []error{nil, domain.ErrProviderTimeout}}
	out, err = Complete(context.Background(), failing, maleRussianPrompt(), true)
	if err != nil || out.Text != original || out.RepairError != "AI_TIMEOUT" {
		t.Fatalf("a failed repair call must not fail the reply: %+v %v", out, err)
	}

	truncated := &scriptedProvider{outputs: []Completion{{Text: original}, {Text: "Пожалуйста! Рад был", Truncated: true}}}
	if out, _ := Complete(context.Background(), truncated, maleRussianPrompt(), true); out.Repaired {
		t.Fatal("a truncated repair must not be used")
	}
}

func TestCompleteSkipsTheRepairWhenDisabledOrClean(t *testing.T) {
	disabled := &scriptedProvider{outputs: []Completion{{Text: "Пожалуйста! Рада была помочь."}}}
	out, err := Complete(context.Background(), disabled, maleRussianPrompt(), false)
	if err != nil || len(disabled.prompts) != 1 || out.RepairAttempted || len(out.Issues) != 1 {
		t.Fatalf("repair disabled: %+v %v", out, err)
	}

	clean := &scriptedProvider{outputs: []Completion{{Text: "Пожалуйста! Рад был помочь."}}}
	if out, _ := Complete(context.Background(), clean, maleRussianPrompt(), true); len(clean.prompts) != 1 || out.Version != PromptVersionReply {
		t.Fatalf("a clean reply must not be repaired: %+v", out)
	}

	down := &scriptedProvider{errs: []error{domain.ErrProviderDown}}
	if _, err := Complete(context.Background(), down, maleRussianPrompt(), true); !errors.Is(err, domain.ErrProviderDown) {
		t.Fatalf("provider error = %v", err)
	}
}

// A Kazakh message answered in Russian (the instruction was a Russian quick
// action) is rewritten into Kazakh by the single repair call.
func TestCompleteRewritesARussianAnswerToAKazakhMessage(t *testing.T) {
	prompt := BuildPrompt(PromptInput{Message: "Ертең кездесуге уақытың бар ма?", Instruction: "Ответь согласием.",
		TemplateID: "friend", AppLanguage: "ru", InputLanguage: "ru"})
	russian, kazakh := "Да, конечно, давай встретимся!", "Иә, әрине, кездесейік!"
	provider := &scriptedProvider{outputs: []Completion{{Text: russian}, {Text: kazakh}}}
	out, err := Complete(context.Background(), provider, prompt, true)
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != kazakh || !out.Repaired || !reflect.DeepEqual(out.Issues, []Issue{IssueLanguage}) {
		t.Fatalf("outcome = %+v", out)
	}
	repair := provider.prompts[1]
	if !strings.Contains(repair.Developer, "The message must be in Kazakh") ||
		!strings.Contains(repair.Developer, languageRules["kk"]) || !strings.Contains(repair.User, russian) {
		t.Fatalf("repair prompt: %+v", repair)
	}

	// The instruction asked for Russian: the Russian answer is what the user wanted.
	named := BuildPrompt(PromptInput{Message: "Ертең кездесуге уақытың бар ма?", Instruction: "Ответь на русском, что приду",
		TemplateID: "friend", AppLanguage: "kk", InputLanguage: "kk"})
	allowed := &scriptedProvider{outputs: []Completion{{Text: "Да, приду!"}}}
	if out, err := Complete(context.Background(), allowed, named, true); err != nil || out.Text != "Да, приду!" ||
		len(allowed.prompts) != 1 || len(out.Issues) != 0 {
		t.Fatalf("a requested language must not be repaired: %+v %v", out, err)
	}
}
