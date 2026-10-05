package ai

import (
	_ "embed"
	"strings"
	"unicode"
)

// WORD EVIDENCE for language detection. Every word of a text adds evidence to
// at most one language, strongest first:
//
//  1. Kazakh letters (ә ғ қ ң ө ұ ү һ і): strong Kazakh evidence.
//  2. Hand-picked marker words: Kazakh typed on a Russian layout ("калайсын",
//     "рахмет"), Russian and English function words, Uzbek Latin words.
//  3. The frequency lists in langdata/: a word found in only one list counts
//     for that language; a word found in both counts for the language where it
//     is far more frequent ("как" → Russian, "бар" → Kazakh), or for neither.
//  4. A word in no list: a typical Kazakh ending typed without Kazakh letters
//     ("кайдасын") or a typical Russian ending ("получится") counts lightly;
//     any other Cyrillic word counts very lightly towards Russian.
//
// A capitalised Kazakh word without a Kazakh ending («Айгүл», «Нурлан»,
// «Алматы») is taken for a name: Russian messages mention them just as often,
// so it counts only a little. Chat tokens that every language uses ("ok",
// «ок», «хаха») count for nothing.

// The frequency lists come from tools/dictionaries/build_dictionaries.py (Leipzig
// Corpora Collection, CC BY): one lowercase word per line, most frequent
// first, '#' lines are comments. Regenerate them with the tool; never edit
// them by hand.
var (
	//go:embed langdata/ru.txt
	russianListData string
	//go:embed langdata/kk.txt
	kazakhListData string
	//go:embed langdata/en.txt
	englishListData string

	russianRanks = frequencyRanks(russianListData, nil)
	kazakhRanks  = frequencyRanks(kazakhListData, kazakhPlain)
	englishRanks = frequencyRanks(englishListData, nil)
)

// Weights of one word's evidence.
const (
	kazakhLetterWeight = 2.5
	nameWeight         = 0.3 // too little to decide a text on its own
	uzbekLetterWeight  = 2.5
	markerWeight       = 1.5
	listWeight         = 1.0
	shortWordWeight    = 0.5 // a Russian or English word of one or two letters («да», «на», "to")
	bareQWeight        = 1.0 // q without u after it: Uzbek Latin
	endingWeight       = 0.6
	unknownWordWeight  = 0.2

	// rankAdvantage — a word in both frequency lists counts for the language
	// where its rank is at least this many times better.
	rankAdvantage = 4
)

const kazakhLetters = "әғқңөұүһі"

// kazakhPlain — the plain letters people type for the Kazakh ones on a Russian
// layout; the same mapping the dictionary tool uses.
var kazakhPlain = strings.NewReplacer("ә", "а", "ғ", "г", "қ", "к", "ң", "н", "ө", "о", "ұ", "у", "ү", "у", "һ", "х", "і", "и")

var (
	// kazakhMarkers — Kazakh words that are not Russian words. The forms with
	// Kazakh letters are listed so that a capitalised one («Сәлем», «Ертең»)
	// is never mistaken for a name.
	kazakhMarkers = wordSet("иа", "ия", "жок", "рахмет", "рахмат", "жарайды", "мен", "сен", "сиз", "биз",
		"калай", "калайсын", "калайсыз", "кайда", "кайдасын", "кашан", "кандай", "жаксы", "бугин", "ертен",
		"кеше", "кешке", "емес", "деп", "ушин", "бирак", "керек", "болады", "салем",
		"ма", "ме", "ба", "бе", "па", "пе",
		"иә", "жоқ", "рақмет", "сәлем", "сіз", "біз", "қалай", "қайда", "қашан", "қандай", "жақсы",
		"бүгін", "ертең", "қазір", "әрине", "мүмкін", "үшін", "бірақ", "қайырлы")
	russianMarkers = wordSet("и", "в", "на", "я", "ты", "вы", "мы", "он", "у", "с", "к", "о",
		"по", "за", "из", "да", "а", "но", "же", "бы", "ли",
		"что", "это", "как", "привет", "спасибо", "когда", "где", "можно", "будет", "есть", "уже",
		"еще", "очень", "давно", "сегодня", "завтра", "пожалуйста", "хорошо", "ладно", "тоже",
		"только", "если", "или", "нет", "вот", "так", "мне", "меня", "тебя", "тебе", "вас", "вам",
		"она", "они", "был", "была", "были", "буду", "будем", "сейчас", "здравствуйте", "добрый",
		"все", "просто", "конечно", "надо", "нужно", "может", "могу", "хочу", "давай",
		"давайте", "почему", "зачем", "сколько", "тут", "там")
	englishMarkers = wordSet("i", "a", "an", "to", "it", "is", "we", "me", "my", "do", "no", "so",
		"in", "on", "at", "be", "of", "or", "if", "us", "he",
		"the", "you", "your", "are", "was", "will", "can", "could", "would", "what", "when",
		"where", "how", "why", "who", "this", "that", "with", "for", "have", "has", "not", "yes",
		"thanks", "thank", "hello", "hey", "please", "sorry", "sure", "tomorrow", "today",
		"tonight", "meeting", "time", "good", "great")
	uzbekMarkers = wordSet("va", "men", "siz", "rahmat", "qanday", "yaxshi", "salom", "ertaga",
		"bugun", "nima", "qachon", "qayerda", "kerak", "emas", "bor")

	// neutralWords — chat tokens that say nothing about the language.
	// «Не» is "not" in Russian and "what" in Kazakh («Не болды?»); «ага» is
	// "uh-huh" in Russian and "older brother" (аға) in Kazakh.
	neutralWords = wordSet("ок", "окей", "оке", "ok", "okay", "okey", "ха", "хаха", "ахах", "ахаха",
		"хм", "мм", "лол", "lol", "haha", "hm", "hmm", "не", "ага")
)

var (
	// plainKazakhEndings — Kazakh person, genitive and plural endings as they
	// look without Kazakh letters ("кайдасын", "жатырмын", "балалар"). An
	// unknown word with one counts a little towards Kazakh.
	plainKazakhEndings = []string{
		"сын", "син", "сыз", "сиз", "мын", "мин", "пын", "пин", "бын", "бин", "мыз", "миз", "ныз", "низ",
		"нын", "нин", "дын", "дин", "тын", "тин", "лар", "лер", "дар", "дер", "тар", "тер"}
	// kazakhGrammarEndings — plainKazakhEndings plus case, possessive and
	// past-tense endings, in plain letters. A capitalised word that has one
	// («Түсіндім», «Алматыға») is a word, not a name. Many Russian words end
	// the same way, so these never count as evidence by themselves.
	kazakhGrammarEndings = append([]string{
		"ын", "ин", "га", "ка", "ды", "ди", "дым", "дим", "тым", "тим",
		"тты", "тти", "пты", "пти", "кты", "кти", "сты", "сти", "шты", "шти",
		"нда", "нде", "дан", "ден", "нан", "нен"}, plainKazakhEndings...)
	// russianEndings — verb, adjective and noun endings Kazakh does not use.
	russianEndings = []string{
		"ться", "тся", "лся", "лась", "лись", "лось", "ешь", "ете", "ишь", "ите",
		"ает", "яет", "ует", "ают", "яют", "уют", "ого", "его", "ому", "ему", "ыми", "ими",
		"ный", "ная", "ное", "ные", "ных", "ным", "ание", "ение", "ость",
		"ский", "ская", "ское", "ские", "ских", "ской"}
)

// Endings say something only on words long enough to have a stem.
const (
	minPlainKazakhLength = 5
	minRussianLength     = 4
)

// evidence — the weight each language gathered from a text.
type evidence struct {
	kk, ru, en, uz float64
	// kazakhWords — a word with Kazakh letters that is not a name.
	kazakhWords bool
}

func gatherEvidence(text string) evidence {
	var e evidence
	for _, t := range tokenize(text) {
		switch {
		case neutralWords[t.word]:
		case isCyrillic(t.word):
			e.addCyrillic(t)
		case isLatin(t.word):
			e.addLatin(t)
		}
	}
	return e
}

func (e *evidence) addCyrillic(t token) {
	w := t.word
	switch {
	case strings.ContainsAny(w, kazakhLetters):
		if isKazakhName(t) {
			e.kk += nameWeight
			return
		}
		e.kk += kazakhLetterWeight
		e.kazakhWords = true
	case kazakhMarkers[w]:
		// Kazakh particles (ма, бе, иа) are not Russian words: full weight.
		e.kk += markerWeight
	case russianMarkers[w]:
		e.ru += shortAware(w, markerWeight)
	default:
		switch lang, found := cyrillicListLanguage(w); {
		case lang == "kk" && isKazakhName(t):
			e.kk += nameWeight
		case lang == "kk":
			e.kk += shortAware(w, listWeight)
		case lang == "ru":
			e.ru += shortAware(w, listWeight)
		case found:
			// Frequent in both languages: no evidence.
		case hasEnding(w, plainKazakhEndings, minPlainKazakhLength):
			e.kk += endingWeight
		case hasEnding(w, russianEndings, minRussianLength):
			e.ru += endingWeight
		default:
			e.ru += unknownWordWeight
		}
	}
}

func (e *evidence) addLatin(t token) {
	w := t.word
	switch {
	case hasUzbekApostrophe(w):
		e.uz += uzbekLetterWeight
	case uzbekMarkers[w]:
		e.uz += shortAware(w, markerWeight)
	case englishMarkers[w]:
		e.en += shortAware(w, markerWeight)
	case hasBareQ(w):
		e.uz += bareQWeight
	case englishRanks[w] > 0:
		e.en += shortAware(w, listWeight)
	default:
		e.en += unknownWordWeight
	}
}

// isKazakhName — a capitalised Kazakh word that is neither a marker nor has a
// Kazakh ending: «Айгүл», «Нұрлан», «Нурлан», «Алматы».
func isKazakhName(t token) bool {
	return t.capitalized && !kazakhMarkers[t.word] && !hasEnding(kazakhPlain.Replace(t.word), kazakhGrammarEndings, 0)
}

// cyrillicListLanguage — which frequency list claims the word: kk or ru, ""
// when both list it at similar ranks. found=false when neither has it.
func cyrillicListLanguage(word string) (lang string, found bool) {
	kk, inKazakh := kazakhRanks[word]
	ru, inRussian := russianRanks[word]
	switch {
	case inKazakh && inRussian:
		if kk*rankAdvantage <= ru {
			return "kk", true
		}
		if ru*rankAdvantage <= kk {
			return "ru", true
		}
		return "", true
	case inKazakh:
		return "kk", true
	case inRussian:
		return "ru", true
	}
	return "", false
}

// frequencyRanks — word → rank, 1 being the most frequent. In the Kazakh list
// every word is followed by its plain spelling (қалайсың, калайсын); the plain
// form shares the rank of its original, so ranks compare across lists.
func frequencyRanks(data string, plain *strings.Replacer) map[string]int {
	ranks := make(map[string]int, strings.Count(data, "\n"))
	rank, previous := 0, ""
	for _, line := range strings.Split(data, "\n") {
		word := normalizeWord(strings.TrimSpace(line))
		if word == "" || strings.HasPrefix(word, "#") {
			continue
		}
		if sharesRank := plain != nil && previous != word && plain.Replace(previous) == word; !sharesRank {
			rank++
		}
		previous = word
		if _, seen := ranks[word]; !seen {
			ranks[word] = rank
		}
	}
	return ranks
}

// token — one word of a text, lowercased, with ё written as е.
type token struct {
	word string
	// capitalized — first letter upper case, not the whole word («Айгүл»,
	// not «АЙГҮЛ»).
	capitalized bool
}

// tokenize — the words of a text. An apostrophe inside a word stays in it
// (Uzbek Latin o'zbek, gʻ); everything that is not a letter separates words.
// A Latin i inside a Cyrillic word is the Kazakh і typed on a Latin layout,
// as in older Kazakh texts («бiз»).
func tokenize(text string) []token {
	var out []token
	runes := []rune(text)
	start := -1
	flush := func(end int) {
		original := runes[start:end]
		word := normalizeWord(string(original))
		if strings.Contains(word, "i") && isCyrillicExceptI(word) {
			word = strings.ReplaceAll(word, "i", "і")
		}
		out = append(out, token{
			word:        word,
			capitalized: len(original) > 1 && unicode.IsUpper(original[0]) && strings.IndexFunc(string(original[1:]), unicode.IsLower) >= 0,
		})
		start = -1
	}
	for i, r := range runes {
		inWord := unicode.IsLetter(r) ||
			(isApostrophe(r) && start >= 0 && i+1 < len(runes) && unicode.IsLetter(runes[i+1]))
		switch {
		case inWord && start < 0:
			start = i
		case !inWord && start >= 0:
			flush(i)
		}
	}
	if start >= 0 {
		flush(len(runes))
	}
	return out
}

func normalizeWord(word string) string {
	return strings.ReplaceAll(strings.ToLower(word), "ё", "е")
}

func shortAware(word string, weight float64) float64 {
	if len([]rune(word)) <= 2 {
		return shortWordWeight
	}
	return weight
}

// hasEnding — the word ends with one of the endings and is at least minLength
// letters long.
func hasEnding(word string, endings []string, minLength int) bool {
	if len([]rune(word)) < minLength {
		return false
	}
	for _, ending := range endings {
		if strings.HasSuffix(word, ending) && word != ending {
			return true
		}
	}
	return false
}

func isCyrillic(word string) bool {
	for _, r := range word {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

// isCyrillicExceptI — every letter is Cyrillic or a Latin i («бiз», not «ХVI»).
func isCyrillicExceptI(word string) bool {
	cyrillic := false
	for _, r := range word {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			cyrillic = true
		case r != 'i' && unicode.IsLetter(r):
			return false
		}
	}
	return cyrillic
}

func isLatin(word string) bool {
	for _, r := range word {
		if unicode.Is(unicode.Latin, r) {
			return true
		}
	}
	return false
}

func isApostrophe(r rune) bool {
	return r == '\'' || r == '’' || r == '`' || r == 'ʻ' || r == 'ʼ'
}

// hasUzbekApostrophe — oʻ / gʻ (кез келген апостроф түрімен).
func hasUzbekApostrophe(word string) bool {
	runes := []rune(word)
	for i := 0; i+1 < len(runes); i++ {
		if (runes[i] == 'o' || runes[i] == 'g') && isApostrophe(runes[i+1]) {
			return true
		}
	}
	return false
}

// hasBareQ — ағылшын тілінде q-дан кейін әрдайым u тұрады, өзбек тілінде жоқ.
func hasBareQ(word string) bool {
	runes := []rune(word)
	for i, r := range runes {
		if r == 'q' && (i+1 == len(runes) || runes[i+1] != 'u') {
			return true
		}
	}
	return false
}

func wordSet(list ...string) map[string]bool {
	out := make(map[string]bool, len(list))
	for _, w := range list {
		out[normalizeWord(w)] = true
	}
	return out
}
