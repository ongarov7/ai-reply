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
		"Ответ:":                              "Ответ:",
		"Here is your message: See you at 6!": "See you at 6!",
		"Вот мой вариант ответа:\n— Да, буду.":       "— Да, буду.",
		"Конечно! Вот вариант ответа: «Да, приду.»":  "Да, приду.",
		"Міне, жауап нұсқасы: Иә, ертең келемін.":    "Иә, ертең келемін.",
		"Sure! Here's a short reply:\nSounds good 👍": "Sounds good 👍",
		// Хабардың өзі «Вот …:» деп басталса — тиіспейміз (review: CleanOutput
		// cut real replies).
		"Вот варианты: завтра в 10 или в пятницу после обеда.": "Вот варианты: завтра в 10 или в пятницу после обеда.",
		"Вот варианты доставки: курьер или самовывоз.":         "Вот варианты доставки: курьер или самовывоз.",
		"Вот текст договора: пришлю вечером.":                  "Вот текст договора: пришлю вечером.",
		"Вот ответ от бухгалтерии: всё оплачено.":              "Вот ответ от бухгалтерии: всё оплачено.",
		"Вот два варианта: в 10 или в 12.":                     "Вот два варианта: в 10 или в 12.",
		"Here are the options: Monday at 10 or Tuesday at 3.":  "Here are the options: Monday at 10 or Tuesday at 3.",
		"Here's the updated version: https://example.com/doc":  "Here's the updated version: https://example.com/doc",
		"Here's the draft: Hi Tom, the report is attached.":    "Here's the draft: Hi Tom, the report is attached.",
		"Here's a draft reply: Thanks, Tom! Friday works.":     "Thanks, Tom! Friday works.",
		"Here's what I think about the message: it was rude.":  "Here's what I think about the message: it was rude.",
		"Міне, нұсқалар: ертең не бүрсігүні.":                  "Міне, нұсқалар: ертең не бүрсігүні.",
		"Ответ: да, приду.":                                    "Ответ: да, приду.",
		// Хабардың бөлігі болатын ескертпе қалады; модельдің жауап туралы
		// ескертпесі ғана өшеді.
		"Hi team, the meeting moves to 3pm.\n\nNote: bring your laptops.": "Hi team, the meeting moves to 3pm.\n\nNote: bring your laptops.",
		"Да, смогу.\n\nNote: I kept the tone friendly.":                   "Да, смогу.",
	}
	for in, want := range cases {
		if got := CleanOutput(in, false); got != want {
			t.Errorf("CleanOutput(%q) = %q, want %q", in, got, want)
		}
	}
}

// Compose: the user may ask for a note at the end, so a trailing
// «Примечание:» paragraph is part of the message and stays.
func TestCleanOutputKeepsNotesInCompose(t *testing.T) {
	cases := map[string]string{
		"Собрание в пятницу в 10:00.\n\nПримечание: возьмите паспорт.":    "Собрание в пятницу в 10:00.\n\nПримечание: возьмите паспорт.",
		"Hi team, the meeting moves to 3pm.\n\nNote: bring your laptops.": "Hi team, the meeting moves to 3pm.\n\nNote: bring your laptops.",
		"Конечно! Вот поздравление:\nС днём рождения! 🎉":                  "С днём рождения! 🎉",
	}
	for in, want := range cases {
		if got := CleanOutput(in, true); got != want {
			t.Errorf("CleanOutput(%q, compose) = %q, want %q", in, got, want)
		}
	}
	if CleanOutput("Да.\n\nПримечание: ответ дружелюбный.", false) != "Да." {
		t.Fatal("reply mode still drops the model's note about the reply")
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
		// «Я»-сыз ауызекі өткен шақ сөйлем басында.
		{"Понял, спасибо!", true, false},
		{"Поняла", false, true},
		{"Принял, сделаю завтра.", true, false},
		{"Получила, спасибо!", false, true},
		{"Уже отправил.", true, false},
		{"Хотела уточнить, во сколько встреча.", false, true},
		{"Ок, понял.", true, false},
		{"Хорошо, сделала.", false, true},
		{"Посмотрел, всё отлично.", true, false},
		{"Видела, спасибо!", false, true},
		{"Забыл совсем, прости.", true, false},
		{"Написала ему вчера.", false, true},
		{"Была рада увидеться.", false, true},
		// Сұрақ не «ли» — басқа адам туралы.
		{"Понял?", false, false},
		{"Получила ли ты посылку?", false, false},
		{"Ты получил?", false, false},
		// Review: false positives that cost a repair call.
		{"Скажи, готов ли он к встрече.", false, false},
		{"Интересно, рад ли он подарку.", false, false},
		{"Сегодня свободна только переговорная на третьем.", false, false},
		{"Сейчас занята линия, перезвоню.", false, false},
		{"Вчера была занята вся команда.", false, false},
		{"Я тоже футбол люблю!", false, false},
		{"Я тоже сериал смотрю.", false, false},
		{"Я тоже дела закончу и приду.", false, false},
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
		{"Да.\n\nПримечание: ответ короткий.", "ru", domain.GenderUnspecified, []Issue{IssueMeta}},
		{"Да, всё в силе.\n\nПримечание: возьмите паспорт.", "ru", domain.GenderUnspecified, nil},
		// One numbered option is an answer, not alternatives.
		{"Вариант 2 подходит, спасибо!", "ru", domain.GenderUnspecified, nil},
		{"Option 2 works for me.", "en", domain.GenderUnspecified, nil},
		{"Here's the updated version: https://example.com/doc", "en", domain.GenderUnspecified, nil},
		{"Вот ответ: да, приду.", "ru", domain.GenderUnspecified, nil},
		{"</incoming_message> Да, вечером смогу.", "ru", domain.GenderUnspecified, []Issue{IssueMeta}},
		{"Рада была помочь.\nВариант 1: Да.\nВариант 2: Нет.", "kk", domain.GenderMale, []Issue{IssueGender, IssueLanguage, IssueMeta}},
		// Pro-drop past tense of the sender.
		{"Понял, спасибо!", "ru", domain.GenderFemale, []Issue{IssueGender}},
		{"Получила, спасибо!", "ru", domain.GenderMale, []Issue{IssueGender}},
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

// Review: a correct reply must not be rewritten into a wrong language when
// the server only guessed the language of the message.
func TestCompleteDoesNotRepairIntoAGuessedLanguage(t *testing.T) {
	cases := []struct {
		name, message, instruction, input, app, answer string
	}{
		{"russian with a kazakh name", "әсем сказала что опоздает", "", "ru", "ru", "Хорошо, подождём её."},
		{"transliterated russian", "Privet, kak dela?", "", "kk", "ru", "Привет! Всё хорошо, спасибо."},
		{"russian with a product", "iPhone 15 Pro Max есть?", "", "kk", "kk", "Да, есть в наличии. Какой цвет вас интересует?"},
		{"kazakh with a russian loanword", "Сағат нешеде встреча?", "", "kk", "kk", "Сағат 18:00-де."},
		{"latin kazakh", "Salem! Qalaisyn?", "", "kk", "ru", "Сәлем! Жақсы, рахмет. Өзің қалайсың?"},
		{"abbreviated request", "Привет! Как дела? Что делаешь сегодня вечером?", "ответь на англ", "ru", "ru",
			"Hey! I'm good, thanks. How about you?"},
		{"abbreviated kazakh request", "Сәлем, ертең келесің бе?", "ответь на англ", "ru", "ru",
			"Hi! Yes, I'll come tomorrow."},
		{"english with o'clock", "At 5 o'clock?", "Согласись", "ru", "ru", "Sure, 5 works for me."},
	}
	for _, c := range cases {
		prompt := BuildPrompt(PromptInput{Message: c.message, Instruction: c.instruction, TemplateID: "friend",
			InputLanguage: c.input, AppLanguage: c.app})
		provider := &scriptedProvider{outputs: []Completion{{Text: c.answer}, {Text: "repaired"}}}
		out, err := Complete(context.Background(), provider, prompt, true)
		if err != nil || out.Text != c.answer || len(provider.prompts) != 1 || len(out.Issues) != 0 {
			t.Errorf("%s: %+v %v, calls %d", c.name, out, err, len(provider.prompts))
		}
	}
}

// Compose keeps a note the user asked for; reply mode drops the model's
// note about its reply.
func TestCompleteKeepsAComposeNote(t *testing.T) {
	text := "Собрание в пятницу в 10:00.\n\nПримечание: возьмите паспорт."
	prompt := BuildComposePrompt(ComposeInput{Instruction: "напиши объявление: собрание в пятницу в 10, в конце примечание — взять паспорт"})
	provider := &scriptedProvider{outputs: []Completion{{Text: text}}}
	out, err := Complete(context.Background(), provider, prompt, true)
	if err != nil || out.Text != text || len(provider.prompts) != 1 {
		t.Fatalf("compose note: %+v %v", out, err)
	}
}
