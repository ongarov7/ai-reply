package ai

import (
	"sort"
	"strings"
)

// LANGUAGE DETECTION works on words, not letters (language_words.go says how
// each word counts). The result serves two purposes: choosing the language a
// reply is written in (only when Confident) and reporting detected_language
// to the client (always the best guess).
//
// Kazakh comes first. People in Kazakhstan mix Russian words into Kazakh
// messages far more often than the reverse, so a message with real Kazakh
// words (not just a Kazakh name) is Kazakh as soon as its Kazakh evidence
// reaches kazakhMixedShare of the Russian evidence. A message without any
// Kazakh signal stays Russian.

// Detection — мәтін тілінің бағасы.
type Detection struct {
	// Lang — kk | ru | en | uz; мәтінде дәлел болмаса бос.
	Lang string
	// Confident — мәтіннің өзі тілді шешеді: дәлел жеткілікті және бір тіл
	// анық басым. Эмодзи, сан, «ok» не «да» сияқты жалғыз сөз — сенімсіз.
	Confident bool
}

const (
	// A language is confident when it has at least dominance times the
	// evidence of the next one and leads it by at least minimumLead.
	dominance   = 2.0
	minimumLead = 0.6
	// kazakhMixedShare — Kazakh words plus at least this share of the Russian
	// evidence make a mixed message Kazakh.
	kazakhMixedShare = 0.75
)

// Detect — мәтін қай тілде жазылғанын бағалайды.
func Detect(text string) Detection {
	return gatherEvidence(text).decide()
}

func (e evidence) decide() Detection {
	latin := max(e.en, e.uz)
	if e.kazakhWords && e.kk >= kazakhMixedShare*e.ru && e.kk >= latin {
		return Detection{Lang: "kk", Confident: leads(e.kk, latin)}
	}
	ranked := []languageScore{{"kk", e.kk}, {"ru", e.ru}, {"en", e.en}, {"uz", e.uz}}
	// Тұрақты сұрыптау: тең ұпайда тізімдегі рет шешеді.
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	best, second := ranked[0], ranked[1]
	if best.score == 0 {
		return Detection{}
	}
	return Detection{Lang: best.lang, Confident: leads(best.score, second.score)}
}

// leads — the best score is clearly ahead of the runner-up.
func leads(best, second float64) bool {
	return best >= dominance*second && best-second >= minimumLead
}

type languageScore struct {
	lang  string
	score float64
}

// DetectLanguage — мәтіннің ең ықтимал тілі (клиентке detected_language).
func DetectLanguage(text string) string { return Detect(text).Lang }

// MentionsLanguage — мәтін тілді атай ма («на казахском», «қазақша», "in
// English"). Атаса, нұсқау жауап тілін өзі таңдайды: шығыс тілі тексерілмейді.
// Ел атаулары (Казахстан, Uzbekistan) тіл атамайды.
func MentionsLanguage(text string) bool {
	for _, t := range tokenize(text) {
		if namesLanguage(t.word) {
			return true
		}
	}
	return false
}

// languageStems — a word starting with one of these names a language:
// казахский, на казахском, қазақша, казакша, по-русски, орысша, английский,
// ағылшынша, in English, узбекский, o'zbekcha.
var languageStems = []string{
	"казах", "қазақ", "казак", "kazakh", "qozoq",
	"русск", "орыс", "russian", "ruscha",
	"английск", "англиск", "ағылшын", "агылшын", "english", "ingliz",
	"узбек", "өзбек", "uzbek", "o'zbek", "oʻzbek", "ozbek",
}

func namesLanguage(word string) bool {
	if strings.Contains(word, "стан") || strings.Contains(word, "stan") {
		return false
	}
	for _, stem := range languageStems {
		if strings.HasPrefix(word, stem) {
			return true
		}
	}
	return false
}

// LanguageTarget — жауап жазылатын тіл және ол неден шешілді.
type LanguageTarget struct {
	// Lang — kk | ru | en | uz; бос болса тіл белгісіз.
	Lang string
	// Source — preference | message | request | keyboard | app.
	Source string
}

// Тіл таңдауының көздері (журналға language_source болып түседі).
const (
	SourcePreference = "preference" // profile.reply_language
	SourceMessage    = "message"    // келген хабарламаның тілі
	SourceRequest    = "request"    // compose нұсқауының тілі
	SourceKeyboard   = "keyboard"   // input_language — Reply басылғандағы пернетақта
	SourceApp        = "app"        // қолданба интерфейсінің тілі
)

// Firm — тіл тұрақты таңдаудан не мәтіннің өзінен анық шықты. Пернетақта мен
// интерфейс тілі — тек болжам: промпт оларды «тіл анық болмаса» деп береді.
func (t LanguageTarget) Firm() bool {
	return t.Source == SourcePreference || t.Source == SourceMessage || t.Source == SourceRequest
}

// InputLanguages — пернетақта орналасуы (input_language) бола алатын кодтар.
var InputLanguages = map[string]bool{"kk": true, "ru": true, "en": true}

// InputLanguage — тексерілген пернетақта коды; белгісіз мән бос қайтады
// (сұраныс қабылданады, өріс жоқ деп саналады).
func InputLanguage(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if InputLanguages[code] {
		return code
	}
	return ""
}

// ResolveReplyTarget — жауап тілі: тұрақты таңдау → келген хабарламаның анық
// тілі → пернетақта → интерфейс → белгісіз.
func ResolveReplyTarget(preference, message, inputLanguage, appLanguage string) LanguageTarget {
	if _, ok := ReplyLanguages[preference]; ok {
		return LanguageTarget{Lang: preference, Source: SourcePreference}
	}
	if d := Detect(message); d.Confident {
		return LanguageTarget{Lang: d.Lang, Source: SourceMessage}
	}
	return fallbackTarget(inputLanguage, appLanguage)
}

// ResolveComposeTarget — жаңа хабарлама тілі: нұсқаудың анық тілі →
// пернетақта → интерфейс → белгісіз.
func ResolveComposeTarget(instruction, inputLanguage, appLanguage string) LanguageTarget {
	if d := Detect(instruction); d.Confident {
		return LanguageTarget{Lang: d.Lang, Source: SourceRequest}
	}
	return fallbackTarget(inputLanguage, appLanguage)
}

func fallbackTarget(inputLanguage, appLanguage string) LanguageTarget {
	if code := InputLanguage(inputLanguage); code != "" {
		return LanguageTarget{Lang: code, Source: SourceKeyboard}
	}
	if code := appLanguageCode(appLanguage); code != "" {
		return LanguageTarget{Lang: code, Source: SourceApp}
	}
	return LanguageTarget{}
}

// appLanguageCode — "ru", "ru-KZ", "kk_KZ" … ғана танылады. Бос не белгісіз
// мән ештеңе бермейді (NormalizeLocale-дегідей "en"-ге түспейді).
func appLanguageCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if len(code) > 2 && (code[2] == '-' || code[2] == '_') {
		code = code[:2]
	}
	if _, ok := ReplyLanguages[code]; ok {
		return code
	}
	return ""
}
