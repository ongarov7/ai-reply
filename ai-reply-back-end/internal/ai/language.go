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
// PRECISION FIRST. A confident result makes the server name the reply
// language in the prompt and repair a reply written in another one, so a
// confidently wrong result forces a wrong-language reply. That is worse than
// no result: without one the prompt asks the model to write in the language
// of the incoming message, and the model mirrors it well. So a result is
// confident only on real evidence: Latin text is confidently English only
// with English function words or list words, confidently Uzbek only with oʻ/gʻ
// or Uzbek words; Russian typed in Latin letters, unknown words and a lone
// Kazakh word that may be a name are never enough.
//
// Kazakh comes first. People in Kazakhstan mix Russian words into Kazakh
// messages far more often than the reverse, so a message with real Kazakh
// words (not just a Kazakh name) is Kazakh as soon as its Kazakh evidence
// reaches kazakhMixedShare of the Russian evidence. It is confidently Kazakh
// only when two Kazakh words, or as much Kazakh as Russian evidence, back it
// up; a lighter mix stays a best guess and the prompt's mixed-message rule
// applies. A message without any Kazakh signal stays Russian.

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
	// minimumLatinEvidence — English or Uzbek is confident only with at least
	// one marker (or two English list words) behind it: one Latin word in a
	// list may as well be a Kazakh or a transliterated Russian word.
	minimumLatinEvidence = markerWeight
)

// Detect — мәтін қай тілде жазылғанын бағалайды.
func Detect(text string) Detection {
	return gatherEvidence(text).decide()
}

func (e evidence) decide() Detection {
	latin := max(e.en, e.uz)
	if e.kazakhWords && e.kk >= kazakhMixedShare*e.ru && e.kk >= latin {
		return Detection{Lang: "kk", Confident: e.kazakhConfident(latin)}
	}
	ranked := []languageScore{{"kk", e.kk, e.weakKK}, {"ru", e.ru, e.weakRU}, {"en", e.en, e.weakEN}, {"uz", e.uz, e.weakUZ}}
	// Тұрақты сұрыптау: тең ұпайда тізімдегі рет шешеді.
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	best, second := ranked[0], ranked[1]
	if best.score == 0 {
		return Detection{}
	}
	strong := best.score - best.weak
	confident := leads(strong, second.score)
	switch best.lang {
	case "en", "uz":
		confident = confident && strong >= minimumLatinEvidence
	case "kk":
		confident = confident && e.kazakhConfident(second.score)
	}
	return Detection{Lang: best.lang, Confident: confident}
}

// kazakhConfident — the Kazakh evidence is real and backed up: not just one
// word that may be a name («Айгүл!»), and against Russian words at least two
// Kazakh words or as much Kazakh as Russian evidence («Сәлем, как дела?»,
// «Түсіндім, спасибо»), and not only possible names.
func (e evidence) kazakhConfident(runnerUp float64) bool {
	if e.kazakhCount == 0 || !leads(e.kk-e.weakKK, runnerUp) {
		return false
	}
	if e.russianCount == 0 {
		return e.kazakhCount > 1 || e.kazakhNameLike == 0
	}
	return (e.kazakhCount >= 2 || e.kk >= e.ru) && (e.kazakhNameLike < e.kazakhCount || e.kazakhCount >= 3)
}

// leads — the best score is clearly ahead of the runner-up.
func leads(best, second float64) bool {
	return best >= dominance*second && best-second >= minimumLead
}

type languageScore struct {
	lang        string
	score, weak float64
}

// DetectLanguage — мәтіннің ең ықтимал тілі (клиентке detected_language).
func DetectLanguage(text string) string { return Detect(text).Lang }

// MentionsLanguage — нұсқау тілді атай ма («на казахском», «қазақша», "in
// English", «на англ», "in eng", "qazaqsha"). Атаса, нұсқау жауап тілін өзі
// таңдайды: шығыс тілі тексерілмейді. Recall matters more than precision
// here: a false match only switches the language check off, while a missed
// request makes the repair rewrite the reply back into the other language.
// Ел атаулары (Казахстан, Uzbekistan, Англия) мен тегтер (Казаков,
// Орысбаев) тіл атамайды.
func MentionsLanguage(text string) bool {
	for _, t := range tokenize(stripNonWords(text)) {
		if namesLanguage(t.word) {
			return true
		}
	}
	return false
}

// languageStems — a word starting with one of these names a language:
// казахский, на казахском, қазақша, казакша, kazaksha, qazaqsha, по-русски,
// орысша, orysha, английский, на англ, англиш, инглиш, english, ағылшынша,
// ingliz, узбекский, o'zbekcha, өзбекше.
var languageStems = []string{
	"казах", "қазақ", "казак", "kazakh", "kazak", "kazah", "qazaq", "qozoq",
	"русск", "орыс", "russian", "russk", "ruscha", "orys",
	"англ", "ағылшын", "агылшын", "english", "engl", "ingliz", "inglish", "инглиш", "angli",
	"узбек", "өзбек", "ўзбек", "uzbek", "o'zbek", "oʻzbek", "ozbek",
}

// languageAbbreviations — short names that count only as a whole word: «на
// каз», «на рус», "in eng", "ru", "kz". As prefixes they would match казино,
// русло or rust.
var languageAbbreviations = wordSet("каз", "қаз", "кз", "рус", "анг", "узб", "eng", "kaz", "kz", "rus", "ru", "uzb", "uz")

// notLanguageNames — words a stem matches that name a country, not a
// language.
var notLanguageNames = wordSet("англия", "англии", "англию", "англией", "england", "angliya", "anglia")

// surnameEndings — «Казаков», «Казаковой», «Орысбаев»: a surname, not a
// language.
var surnameEndings = []string{"ов", "ова", "ову", "ове", "овой", "овым", "ев", "ева", "еву", "еве", "евой", "евым"}

func namesLanguage(word string) bool {
	if languageAbbreviations[word] {
		return true
	}
	if strings.Contains(word, "стан") || strings.Contains(word, "stan") || notLanguageNames[word] ||
		hasEnding(word, surnameEndings, 0) {
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
