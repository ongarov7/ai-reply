// Package eval — AI жауаптарының сапасын бағалау жиынтығы.
//
// The suite checks behaviour, never exact sentences: the language of the
// output, the sender's grammatical gender in Russian, leftover labels or
// preambles, length, details that must survive, words that must not appear,
// ты/вы and сен/Сіз, real Kazakh letters, and no question in a refusal.
// cases.json holds the cases; Evaluate compares one output with one case.
// The offline tests prove every check on known-good and known-bad outputs;
// the live test (AIREPLY_LIVE_EVAL=1) runs the real model through the
// production prompt builders. See docs/AI_QUALITY.md.
package eval

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

//go:embed cases.json
var casesJSON []byte

// Case — бір бағалау жағдайы.
type Case struct {
	ID string `json:"id"`
	// Mode — reply | compose.
	Mode        string `json:"mode"`
	Incoming    string `json:"incoming"`
	Instruction string `json:"instruction"`
	// TemplateID — friend | client | business | work (reply үшін).
	TemplateID string `json:"template_id"`
	// Gender — male | female | unspecified.
	Gender string `json:"gender"`
	// InputLanguage — kk | ru | en не бос.
	InputLanguage string `json:"input_language"`
	// AppLanguage — қолданба тілі; бос болса InputLanguage-пен бірдей.
	AppLanguage string `json:"app_language,omitempty"`
	// Mirror — сервер хабарламаның тілін сенімді анықтай алмайды (мысалы,
	// латынмен терілген орысша): промпт модельге хабарламаның тілінде жазуды
	// айтады, тіл тексерілмейді. Expectations.Language — бәрібір күтілетін тіл.
	Mirror       bool         `json:"mirror,omitempty"`
	Expectations Expectations `json:"expectations"`
}

// Expectations — жауап сай болуы керек мінез-құлық. Бос өріс тексерілмейді.
type Expectations struct {
	// Language — шығыстың анықталған тілі.
	Language string `json:"language,omitempty"`
	// GenderAgreement — жіберушіге қатысты орысша тұлғалар жынысқа сай;
	// unspecified болса — жыныс көрсететін тұлға мүлде жоқ.
	GenderAgreement bool `json:"gender_agreement,omitempty"`
	// NoMeta — жапсырма, кіріспе, балама нұсқа не ескерту жоқ.
	NoMeta       bool `json:"no_meta,omitempty"`
	MaxSentences int  `json:"max_sentences,omitempty"`
	MaxChars     int  `json:"max_chars,omitempty"`
	// MustIncludeAny — кемінде біреуі сөз басынан табылуы керек («уточн» →
	// «уточню»), регистр ескерілмейді.
	MustIncludeAny []string `json:"must_include_any,omitempty"`
	// MustNotInclude — ешқайсысы кездеспеуі керек (ішкі жол, регистрсіз).
	MustNotInclude []string `json:"must_not_include,omitempty"`
	// NoDigits — сан жоқ (берілмеген баға, күн, уақыт ойдан шығарылмайды).
	NoDigits bool `json:"no_digits,omitempty"`
	// Formality — formal (вы/Сіз) не informal (ты/сен).
	Formality string `json:"formality,omitempty"`
	// KazakhLetters — қазақша жауапта кемінде бір арнайы әріп бар.
	KazakhLetters bool `json:"kazakh_letters,omitempty"`
	// NoQuestionIfDecline — бас тарту жауабы сұрақпен аяқталмайды.
	NoQuestionIfDecline bool `json:"no_question_if_decline,omitempty"`
}

// appLanguage — жағдайдағы қолданба тілі (әдепкіде пернетақта тілі).
func (c Case) appLanguage() string {
	if c.AppLanguage != "" {
		return c.AppLanguage
	}
	return c.InputLanguage
}

// Cases — cases.json ішіндегі барлық жағдай (белгісіз кілт — қате).
func Cases() ([]Case, error) {
	decoder := json.NewDecoder(bytes.NewReader(casesJSON))
	decoder.DisallowUnknownFields()
	var cases []Case
	if err := decoder.Decode(&cases); err != nil {
		return nil, fmt.Errorf("cases.json: %w", err)
	}
	return cases, nil
}

// Тексеру атаулары — cases.json кілттерімен бірдей.
const (
	CheckLanguage       = "language"
	CheckGender         = "gender_agreement"
	CheckNoMeta         = "no_meta"
	CheckMaxSentences   = "max_sentences"
	CheckMaxChars       = "max_chars"
	CheckMustIncludeAny = "must_include_any"
	CheckMustNotInclude = "must_not_include"
	CheckNoDigits       = "no_digits"
	CheckFormality      = "formality"
	CheckKazakhLetters  = "kazakh_letters"
	CheckNoQuestion     = "no_question_if_decline"
)

// Failure — сәтсіз тексеру және оның себебі.
type Failure struct {
	Check  string
	Detail string
}

func (f Failure) String() string { return f.Check + ": " + f.Detail }

// Evaluate — жауап жағдайдың күтулеріне қай жерде сай келмейді (бос — бәрі дұрыс).
func Evaluate(c Case, output string) []Failure {
	e := c.Expectations
	var out []Failure
	fail := func(check, format string, args ...any) {
		out = append(out, Failure{Check: check, Detail: fmt.Sprintf(format, args...)})
	}
	lower := strings.ToLower(output)

	if e.Language != "" {
		if got := ai.DetectLanguage(output); got != e.Language {
			fail(CheckLanguage, "detected %q, want %q", got, e.Language)
		}
	}
	if e.GenderAgreement {
		masculine, feminine := ai.GenderedSelfForms(output)
		switch {
		case c.Gender == domain.GenderMale && feminine:
			fail(CheckGender, "feminine form for a male sender")
		case c.Gender == domain.GenderFemale && masculine:
			fail(CheckGender, "masculine form for a female sender")
		case c.Gender == domain.GenderUnspecified && (masculine || feminine):
			fail(CheckGender, "gendered self-form for an unknown sender")
		}
	}
	if e.NoMeta && slices.Contains(ai.Check(output, "", domain.GenderUnspecified), ai.IssueMeta) {
		fail(CheckNoMeta, "label, preamble, alternatives or a note left in the text")
	}
	if e.MaxSentences > 0 {
		if n := Sentences(output); n > e.MaxSentences {
			fail(CheckMaxSentences, "%d sentences, max %d", n, e.MaxSentences)
		}
	}
	if e.MaxChars > 0 {
		if n := len([]rune(output)); n > e.MaxChars {
			fail(CheckMaxChars, "%d characters, max %d", n, e.MaxChars)
		}
	}
	if len(e.MustIncludeAny) > 0 && !slices.ContainsFunc(e.MustIncludeAny, func(want string) bool {
		return startsWord(lower, strings.ToLower(want))
	}) {
		fail(CheckMustIncludeAny, "none of %q", e.MustIncludeAny)
	}
	for _, banned := range e.MustNotInclude {
		if strings.Contains(lower, strings.ToLower(banned)) {
			fail(CheckMustNotInclude, "contains %q", banned)
		}
	}
	if e.NoDigits && strings.IndexFunc(output, unicode.IsDigit) >= 0 {
		fail(CheckNoDigits, "contains a number nobody gave")
	}
	if e.Formality != "" {
		if word := registerMismatch(lower, e.Formality); word != "" {
			fail(CheckFormality, "%q in a %s message", word, e.Formality)
		}
	}
	if e.KazakhLetters && !strings.ContainsAny(lower, "әғқңөұүһі") {
		fail(CheckKazakhLetters, "no Kazakh-specific letter")
	}
	if e.NoQuestionIfDecline && strings.Contains(output, "?") {
		fail(CheckNoQuestion, "a refusal that asks a question")
	}
	return out
}

var sentenceEnd = regexp.MustCompile(`[.!?…]+(?:\s|$)`)

// Sentences — сөйлем саны: тыныс белгісімен аяқталғандары және соңындағы
// әріпті үзінді (эмодзи жеке сөйлем емес).
func Sentences(text string) int {
	text = strings.TrimSpace(text)
	ends := sentenceEnd.FindAllStringIndex(text, -1)
	count := len(ends)
	tail := text
	if count > 0 {
		tail = text[ends[count-1][1]:]
	}
	if strings.IndexFunc(tail, unicode.IsLetter) >= 0 {
		count++
	}
	return count
}

// startsWord — want мәтінде сөз басынан басталады ма.
func startsWord(text, want string) bool {
	for from := 0; ; {
		at := strings.Index(text[from:], want)
		if at < 0 {
			return false
		}
		at += from
		if at == 0 || !isLetterBefore(text, at) {
			return true
		}
		from = at + len(want)
	}
}

func isLetterBefore(text string, at int) bool {
	r := []rune(text[:at])
	return len(r) > 0 && unicode.IsLetter(r[len(r)-1])
}

var (
	informalWords = map[string]bool{"ты": true, "тебя": true, "тебе": true, "тобой": true, "твой": true,
		"твоя": true, "твоё": true, "твое": true, "твои": true, "твоих": true, "твоим": true, "твоему": true,
		"твоей": true, "сен": true, "сені": true, "саған": true, "сенің": true, "сенде": true, "сенен": true,
		"сенімен": true}
	formalWords = map[string]bool{"вы": true, "вас": true, "вам": true, "вами": true, "ваш": true, "ваша": true,
		"ваше": true, "ваши": true, "вашего": true, "вашей": true, "вашим": true, "ваших": true, "вашему": true,
		"сіз": true, "сізге": true, "сізді": true, "сіздің": true, "сізбен": true, "сізден": true}
)

// registerMismatch — керек деңгейге қайшы бірінші сөз (сай болса бос).
func registerMismatch(lower, formality string) string {
	wrong := informalWords
	if formality == "informal" {
		wrong = formalWords
	}
	for _, word := range strings.FieldsFunc(lower, func(r rune) bool { return !unicode.IsLetter(r) }) {
		if wrong[word] {
			return word
		}
	}
	return ""
}
