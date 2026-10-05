package ai

import (
	_ "embed"
	"regexp"
	"strings"
	"unicode"
)

// WORD EVIDENCE for language detection. Links, e-mail addresses, @handles,
// #tags, numbers, emoji and words glued to digits ("S24", "html5") are removed
// first: they say nothing about the language. Every remaining word adds
// evidence to at most one language, strongest first:
//
//  1. Kazakh letters (ә ғ қ ң ө ұ ү һ і): strong Kazakh evidence; Uzbek
//     Cyrillic letters (ў ҳ) and Uzbek markers: Uzbek.
//  2. Hand-picked marker words: Kazakh typed on a Russian layout ("калайсын",
//     "рахмет") or in Latin letters ("salem", "qalaisyn", "rahmet"), Russian
//     and English function words, Uzbek Latin words.
//  3. The frequency lists in langdata/: a word found in only one list counts
//     for that language; a word found in both counts for the language where it
//     is far more frequent ("как" → Russian, "бар" → Kazakh), or for neither.
//  4. A word in no list: a distinctly Kazakh verb ending typed without Kazakh
//     letters ("бармаймын") counts like a marker; a typical Kazakh ending
//     ("кайдасын") or Russian ending ("получится") counts lightly.
//
// WEAK evidence still picks the best guess, but never makes a result
// confident (language.go): Russian typed in Latin letters ("privet", "kak
// dela"), Latin words in no list, a q without u, the letters of the 2021
// Kazakh Latin alphabet, plain Kazakh endings and unknown Cyrillic words.
// In a message with Cyrillic words, Latin words other than markers are brand
// and product names ("iPhone 15 Pro Max есть?") and count for nothing.
//
// NAMES. A capitalised Kazakh word in the middle of a sentence without a
// Kazakh ending («Айгүл», «Нұрлан», «Алматы») is taken for a name and counts
// only a little. A Kazakh word that could be a name anywhere else (lowercase
// «әсем», or first in the sentence «Әсел, ты где?») is real evidence, but
// when it is the only Kazakh word of a message with Russian words it is taken
// for a name too. Chat tokens that every language uses ("ok", «ок», «хаха»)
// count for nothing.

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
	bareQWeight        = 0.5 // weak, for both Kazakh and Uzbek: q without u is Turkic Latin
	endingWeight       = 0.6
	unknownWordWeight  = 0.2

	// rankAdvantage — a word in both frequency lists counts for the language
	// where its rank is at least this many times better.
	rankAdvantage = 4
	// commonKazakhRank — the most frequent words with Kazakh letters («жыл»,
	// «бұл», «қазір») are words, not names, even without a Kazakh ending.
	// Names of people rank far lower (Нұрлан 1257, Әсем 3647). Without Kazakh
	// letters a frequent word may still be Russian («бар»).
	commonKazakhRank = 200
)

const (
	kazakhLetters = "әғқңөұүһі"
	// kazakhOnlyLetters — Kazakh letters Uzbek Cyrillic does not have (it
	// shares қ and ғ).
	kazakhOnlyLetters = "әңөұүһі"
	// uzbekCyrillicLetters — ў and ҳ: Uzbek, never Kazakh.
	uzbekCyrillicLetters = "ўҳ"
	// kazakhLatinLetters — letters of the 2021 Kazakh Latin alphabet that
	// English and Uzbek do not use.
	kazakhLatinLetters = "áóúýńǵı"
)

// kazakhPlain — the plain letters people type for the Kazakh ones on a Russian
// layout; the same mapping the dictionary tool uses.
var kazakhPlain = strings.NewReplacer("ә", "а", "ғ", "г", "қ", "к", "ң", "н", "ө", "о", "ұ", "у", "ү", "у", "һ", "х", "і", "и")

var (
	// kazakhMarkers — Kazakh words that are not Russian words. The forms with
	// Kazakh letters are listed so that a capitalised one («Сәлем», «Ертең»,
	// «Қанша») is never mistaken for a name.
	kazakhMarkers = wordSet("иа", "ия", "жок", "рахмет", "рахмат", "жарайды", "мен", "сен", "сиз", "биз",
		"калай", "калайсын", "калайсыз", "кайда", "кайдасын", "кашан", "кандай", "жаксы", "бугин", "ертен",
		"кеше", "кешке", "емес", "деп", "ушин", "бирак", "керек", "болады", "салем",
		"ма", "ме", "ба", "бе", "па", "пе",
		"канша", "неше", "нешеде", "нешеге", "нешинши", "неге", "керемет", "тамаша",
		"иә", "жоқ", "рақмет", "сәлем", "сіз", "біз", "қалай", "қайда", "қашан", "қандай", "жақсы",
		"бүгін", "ертең", "қазір", "әрине", "мүмкін", "үшін", "бірақ", "қайырлы",
		"қанша", "нешінші", "қайсы", "сәлеметсіз", "кешіріңіз", "өтінем",
		// What users type in an instruction: «жауап бер», «айт», «жаз».
		"жауап", "айт", "жаз", "айтшы", "жазшы")
	russianMarkers = wordSet("и", "в", "на", "я", "ты", "вы", "мы", "он", "у", "с", "к", "о",
		"по", "за", "из", "да", "а", "но", "же", "бы", "ли",
		"что", "это", "как", "привет", "спасибо", "когда", "где", "можно", "будет", "есть", "уже",
		"еще", "очень", "давно", "сегодня", "завтра", "пожалуйста", "хорошо", "ладно", "тоже",
		"только", "если", "или", "нет", "вот", "так", "мне", "меня", "тебя", "тебе", "вас", "вам",
		"она", "они", "был", "была", "были", "буду", "будем", "сейчас", "здравствуйте", "добрый",
		"все", "просто", "конечно", "надо", "нужно", "может", "могу", "хочу", "давай",
		"давайте", "почему", "зачем", "сколько", "тут", "там", "посмотри", "напиши", "позвони", "скинь", "глянь",
		// What users type in an instruction or a quick action.
		"ответь", "ответьте", "скажи", "откажи", "откажись", "согласись", "поблагодари", "спроси", "уточни",
		"предложи", "извинись", "поздравь", "передай", "напомни", "вежливо", "согласием", "отказом")
	englishMarkers = wordSet("i", "a", "an", "to", "it", "is", "we", "me", "my", "do", "no", "so",
		"in", "on", "at", "be", "of", "or", "if", "us", "he",
		"the", "you", "your", "are", "was", "will", "can", "could", "would", "what", "when",
		"where", "how", "why", "who", "this", "that", "with", "for", "have", "has", "not", "yes",
		"thanks", "thank", "hello", "hey", "please", "sorry", "sure", "tomorrow", "today",
		"tonight", "meeting", "time", "good", "great",
		// Contractions and o'clock: English, not Uzbek oʻ/gʻ.
		"i'm", "i'll", "i've", "i'd", "it's", "that's", "what's", "who's", "where's", "there's",
		"he's", "she's", "let's", "you're", "we're", "they're", "you'll", "we'll", "don't",
		"can't", "won't", "didn't", "isn't", "doesn't", "wasn't", "aren't", "couldn't",
		"wouldn't", "shouldn't", "haven't", "o'clock")
	// uzbekMarkers — Uzbek Latin words that are neither Kazakh nor English.
	// Words Kazakh shares (men, sen, siz, biz) are in latinNeutralWords.
	uzbekMarkers = wordSet("va", "yaxshi", "yaxshimisiz", "salom", "assalomu", "alaykum", "ertaga",
		"bugun", "nima", "nimaga", "qachon", "qayerda", "qayerdasiz", "kerak", "emas", "bor", "hozir",
		"kecha", "uchun", "bilan", "lekin", "juda")
	// uzbekWeakMarkers — Uzbek, but Kazakhs typing in Latin write them too.
	uzbekWeakMarkers = wordSet("rahmat", "qanday", "qalaysiz")
	// kazakhLatinMarkers — Kazakh typed in Latin letters, informally or in
	// the 2021 alphabet. None of them is an English or Uzbek word.
	kazakhLatinMarkers = wordSet("salem", "salemet", "salemetsiz", "salemetsizbe", "sálem", "sálemetsiz",
		"sálemetsizbe", "qalai", "kalai", "qalaı", "qalaisyn", "kalaisyn", "qalaysyn", "kalaysyn",
		"qalaisyz", "kalaisyz", "qalaysyz", "qalaısyń", "qalaısyz", "qalaisyń", "qandai", "kandai", "qandaı",
		"rahmet", "raqmet", "rakhmet", "raxmet", "jaqsy", "jaksy", "zhaksy", "jaqsı", "jaqsymyn",
		"jaksymyn", "qaida", "kaida", "qaıda", "qaidasyn", "kaidasyn", "qaıdasyń", "qaidasyz",
		"kaidasyz", "qashan", "kashan", "erten", "erteń", "bugin", "búgin", "keshe", "keshke", "bolady",
		"boldy", "jaraidy", "jaraıdy", "zharaidy", "kerek", "emes", "kelesin", "kelesiń", "kelesiz",
		"kelemin", "kelem", "keldim", "baramyn", "barasyn", "barasyń", "qazir", "kazir", "iá", "ıá",
		"joq", "jok", "qaiyrly", "qaıyrly", "kaiyrly", "otinem", "ótinem", "keshiriniz", "keshirińiz",
		"kelisemin", "kelistik", "jumys", "zhumys", "uide", "uyde", "úıde", "uıde", "qansha",
		"kansha", "neshede")
	// russianLatinMarkers — Russian typed in Latin letters. Weak: the reply
	// may follow in Latin or in Cyrillic, so the model decides.
	russianLatinMarkers = wordSet("privet", "privetik", "kak", "dela", "delaesh", "spasibo", "spasib",
		"spsb", "chto", "shto", "che", "poka", "davai", "davay", "segodnya", "sevodnya", "segodnia",
		"zavtra", "vchera", "mozhno", "mojno", "khorosho", "horosho", "harasho", "xorosho", "ladno",
		"tebya", "tebe", "menya", "mne", "pozhaluista", "pozhalujsta", "pojaluista", "pozhaluysta",
		"pojaluysta", "seichas", "sejchas", "sechas", "gde", "kogda", "pochemu", "zachem", "skolko",
		"zdravstvuite", "zdravstvuyte", "zdravstvujte", "ochen", "tozhe", "toje", "uzhe", "uje", "eshe",
		"eshche", "esche", "nado", "nuzhno", "mogu", "mozhesh", "hochu", "budu", "budet", "napishi",
		"pozvoni", "perezvoni", "nichego", "normalno", "konechno", "prosto", "rebyata", "slushai",
		"slushay", "kstati", "blin", "chego", "eto", "vse", "vsem", "kto", "tut", "opozdayu", "priedu",
		"pridu", "dobroe", "utro", "vecher", "vecherom", "spokoinoi", "nochi", "kuda", "otvet", "otvet'",
		"napisat", "skazhi", "skaji")
	// uzbekCyrillicMarkers — Uzbek Cyrillic words that are neither Kazakh nor
	// Russian.
	uzbekCyrillicMarkers = wordSet("ассалому", "алайкум", "салом", "эртага", "бугун", "нима", "нимага",
		"қачон", "керак", "эмас", "билан", "учун", "лекин", "жуда", "ишлар", "қалайсиз")
	uzbekCyrillicPrefixes = []string{"яхши", "қаерда"}

	// neutralWords — chat tokens that say nothing about the language.
	// «Не» is "not" in Russian and "what" in Kazakh («Не болды?»); «ага» is
	// "uh-huh" in Russian and "older brother" (аға) in Kazakh.
	neutralWords = wordSet("ок", "окей", "оке", "ok", "okay", "okey", "ха", "хаха", "ахах", "ахаха",
		"хм", "мм", "лол", "lol", "haha", "hm", "hmm", "не", "ага")
	// latinNeutralWords — Latin words Kazakh, Uzbek and English share.
	latinNeutralWords = wordSet("men", "sen", "siz", "biz", "ok", "okay", "okey", "lol", "haha", "hm", "hmm")
)

var (
	// plainKazakhEndings — Kazakh person, genitive and plural endings as they
	// look without Kazakh letters ("кайдасын", "жатырмын", "балалар"). An
	// unknown word with one is weak evidence for Kazakh.
	plainKazakhEndings = []string{
		"сын", "син", "сыз", "сиз", "мын", "мин", "пын", "пин", "бын", "бин", "мыз", "миз", "ныз", "низ",
		"нын", "нин", "дын", "дин", "тын", "тин", "лар", "лер", "дар", "дер", "тар", "тер"}
	// strongPlainKazakhEndings — Kazakh verb forms typed without Kazakh
	// letters that no Russian word has: «бармаймын», «барамын», «келесин».
	strongPlainKazakhEndings = []string{
		"маймын", "меймин", "баймын", "беймин", "паймын", "пеймин", "майсын", "мейсин", "майсыз", "мейсиз",
		"амын", "емин", "ймын", "ймин", "амыз", "емиз", "ймыз", "ймиз", "асын", "есин", "асыз", "есиз",
		"ырсын", "ырмын", "ырсыз", "ырмыз", "ирсин", "ирмин", "ирсиз", "ирмиз"}
	// kazakhGrammarEndings — plainKazakhEndings plus case, possessive and
	// past-tense endings, in plain letters. A capitalised word that has one
	// («Түсиндим», «Алматыга») is a word, not a name. Many Russian words end
	// the same way, so these never count as evidence by themselves.
	kazakhGrammarEndings = append([]string{
		"ын", "ин", "га", "ка", "ге", "ке", "ды", "ди", "дым", "дим", "тым", "тим",
		"тты", "тти", "пты", "пти", "кты", "кти", "сты", "сти", "шты", "шти",
		"нда", "нде", "дан", "ден", "нан", "нен", "тан", "тен", "де", "те", "ны", "ни", "ти",
		"мен", "бен", "пен", "ап", "еп", "ып", "ип"}, plainKazakhEndings...)
	// sureKazakhEndings — endings, written with Kazakh letters, that Kazakh
	// names do not have: a word with Kazakh letters and one of these is a
	// Kazakh word («уақытың», «түсіндім», «үйге», «Алматыға»), not a name.
	// Endings names do have (-тар «Мұхтар», -ды «Қарағанды», -сын «Оңдасын»,
	// -кен «Сәкен») are left out on purpose.
	sureKazakhEndings = []string{
		"ң", "ңыз", "ңіз", "мін", "пін", "бін", "міз", "піз", "біз", "сіз", "мын", "пын", "бын", "мыз", "сыз",
		"дім", "тім", "дық", "дік", "тық", "тік", "ді", "ті", "йды", "ға", "қа", "ге", "ке", "да", "де", "та", "те",
		"ден", "тен", "нен", "нан", "мен", "бен", "пен", "ған", "қан", "лер", "дер", "тер", "іп", "тін", "лық", "лік",
		"ғы", "гі", "қы", "кі", "ны", "ні", "сы", "сі"}
	// russianEndings — verb, adjective and noun endings Kazakh does not use.
	russianEndings = []string{
		"ться", "тся", "лся", "лась", "лись", "лось", "ешь", "ете", "ишь", "ите",
		"ает", "яет", "ует", "ают", "яют", "уют", "ого", "его", "ому", "ему", "ыми", "ими",
		"ный", "ная", "ное", "ные", "ных", "ным", "ание", "ение", "ость",
		"ский", "ская", "ское", "ские", "ских", "ской"}
	// kazakhLatinEndings — Kazakh person endings in Latin letters
	// ("kelesyn", "baramyn"): weak evidence.
	kazakhLatinEndings = []string{"syn", "syz", "myn", "myz", "pyn", "byn", "dym", "tym", "syń", "ıń", "iń"}
	// englishSuffixes — what follows the apostrophe in an English
	// contraction or possessive ("who's", "don't", "Greg's").
	englishSuffixes = map[string]bool{"s": true, "t": true, "d": true, "m": true, "re": true, "ll": true, "ve": true}
)

// Endings say something only on words long enough to have a stem.
const (
	minPlainKazakhLength  = 5
	minStrongKazakhLength = 7
	minRussianLength      = 4
	minLatinEndingLength  = 5
)

// nonWords — links, e-mail addresses, @handles and #tags. They are removed
// before the words are counted.
var nonWords = regexp.MustCompile(`(?i)(?:https?://|www\.)\S+` +
	`|[\p{L}\p{N}._%+-]+@[\p{L}\p{N}.-]+\.\p{L}{2,}` +
	`|[@#][\p{L}\p{N}_.]+` +
	`|\b[a-z0-9][a-z0-9-]*(?:\.[a-z0-9-]+)*\.(?:com|net|org|ru|kz|uz|io|me|app|info|biz|co|tv|ly|gl|gg|ai|dev|shop|store)\b(?:/\S*)?`)

// stripNonWords — replaces links, addresses, handles and tags with spaces.
func stripNonWords(text string) string {
	return nonWords.ReplaceAllString(text, " ")
}

// evidence — the weight each language gathered from a text.
type evidence struct {
	kk, ru, en, uz float64
	// weakKK … weakUZ — the part of each score that never makes a result
	// confident.
	weakKK, weakRU, weakEN, weakUZ float64
	// kazakhWords — a word with Kazakh letters or a distinctly Kazakh verb
	// ending that is not a name: such a message is Kazakh even when it mixes
	// in Russian words (language.go).
	kazakhWords bool
	// kazakhCount — words that gave Kazakh evidence (names excluded);
	// kazakhNameLike — of those, words that could also be a name (no marker,
	// no Kazakh ending): «әсел», «Мұхтар», «Нурлан»; nameLikeWeight — their
	// weight.
	kazakhCount, kazakhNameLike int
	nameLikeWeight              float64
	// russianCount — words that gave real (not weak) Russian evidence.
	russianCount int
}

// textContext — what the whole text holds, needed to score one word.
type textContext struct {
	// cyrillic — the text has Cyrillic words: Latin words in it are names.
	cyrillic bool
	// uzbekCyrillic — the text has Uzbek Cyrillic letters or words: қ and ғ
	// are then Uzbek, not Kazakh.
	uzbekCyrillic bool
}

func gatherEvidence(text string) evidence {
	tokens := tokenize(stripNonWords(text))
	var ctx textContext
	for _, t := range tokens {
		if neutralWords[t.word] || !isCyrillic(t.word) {
			continue
		}
		ctx.cyrillic = true
		if strings.ContainsAny(t.word, uzbekCyrillicLetters) || isUzbekCyrillicMarker(t.word) {
			ctx.uzbekCyrillic = true
		}
	}
	var e evidence
	for _, t := range tokens {
		switch {
		case neutralWords[t.word]:
		case isCyrillic(t.word):
			e.addCyrillic(t, ctx)
		case isLatin(t.word):
			e.addLatin(t, ctx)
		}
	}
	// One Kazakh word that could be a name, among Russian words, is a name:
	// «әсем сказала что опоздает», «Мұхтар, ты где?».
	if e.russianCount > 0 && e.kazakhCount == 1 && e.kazakhNameLike == 1 {
		e.kk += nameWeight - e.nameLikeWeight
		e.kazakhCount, e.kazakhNameLike, e.nameLikeWeight = 0, 0, 0
		e.kazakhWords = false
	}
	return e
}

// addKazakh — one word of Kazakh evidence.
func (e *evidence) addKazakh(weight float64, kazakhWord, nameLike bool) {
	e.kk += weight
	e.kazakhCount++
	if kazakhWord {
		e.kazakhWords = true
	}
	if nameLike {
		e.kazakhNameLike++
		e.nameLikeWeight += weight
	}
}

func (e *evidence) addRussian(weight float64) {
	e.ru += weight
	e.russianCount++
}

func (e *evidence) addCyrillic(t token, ctx textContext) {
	w := t.word
	switch {
	case strings.ContainsAny(w, uzbekCyrillicLetters):
		e.uz += uzbekLetterWeight
	case isUzbekCyrillicMarker(w):
		e.uz += markerWeight
	case strings.ContainsAny(w, kazakhLetters):
		if ctx.uzbekCyrillic && !strings.ContainsAny(w, kazakhOnlyLetters) {
			// Only қ or ғ next to Uzbek words: Uzbek Cyrillic.
			e.uz += listWeight
			return
		}
		sure := kazakhMarkers[w] || hasEnding(w, sureKazakhEndings, 0)
		if !sure && t.capitalized && !t.initial {
			e.kk += nameWeight
			return
		}
		e.addKazakh(kazakhLetterWeight, true, !sure && !isCommonKazakh(w))
	case kazakhMarkers[w]:
		// Kazakh particles (ма, бе, иа) are not Russian words: full weight.
		// A longer marker («рахмет», «керемет», «калайсын») is a Kazakh word
		// like one with Kazakh letters.
		e.addKazakh(markerWeight, len([]rune(w)) >= 4, false)
	case russianMarkers[w]:
		e.addRussian(shortAware(w, markerWeight))
	default:
		switch lang, found := cyrillicListLanguage(w); {
		case lang == "kk":
			noEnding := !hasEnding(w, kazakhGrammarEndings, 0)
			if noEnding && t.capitalized && !t.initial {
				e.kk += nameWeight
				return
			}
			e.addKazakh(shortAware(w, listWeight), false, noEnding)
		case lang == "ru":
			e.addRussian(shortAware(w, listWeight))
		case found:
			// Frequent in both languages: no evidence.
		case hasEnding(w, strongPlainKazakhEndings, minStrongKazakhLength) && !(t.capitalized && !t.initial):
			e.addKazakh(markerWeight, true, false)
		case hasEnding(w, plainKazakhEndings, minPlainKazakhLength):
			e.kk += endingWeight
			e.weakKK += endingWeight
		case hasEnding(w, russianEndings, minRussianLength):
			e.addRussian(endingWeight)
		default:
			e.ru += unknownWordWeight
			e.weakRU += unknownWordWeight
		}
	}
}

func (e *evidence) addLatin(t token, ctx textContext) {
	w := t.word
	switch {
	case latinNeutralWords[w]:
	case hasUzbekApostrophe(t):
		e.uz += uzbekLetterWeight
	case kazakhLatinMarkers[w]:
		e.addKazakh(markerWeight, false, false)
	case uzbekMarkers[w]:
		e.uz += shortAware(w, markerWeight)
	case uzbekWeakMarkers[w]:
		e.uz += markerWeight
		e.weakUZ += markerWeight
	case russianLatinMarkers[w]:
		e.ru += markerWeight
		e.weakRU += markerWeight
	case englishMarkers[w]:
		e.en += shortAware(w, markerWeight)
	case ctx.cyrillic:
		// iPhone, Pro, Samsung, Zoom in a Russian or Kazakh message.
	case strings.ContainsAny(w, kazakhLatinLetters):
		e.kk += endingWeight
		e.weakKK += endingWeight
	case isEnglishListWord(w):
		weight := shortAware(w, listWeight)
		e.en += weight
		if weight < listWeight {
			// "ma", "da", "ne": too short to tell English from Kazakh or Russian.
			e.weakEN += weight
		}
	case hasBareQ(w):
		e.kk += bareQWeight
		e.weakKK += bareQWeight
		e.uz += bareQWeight
		e.weakUZ += bareQWeight
	case hasEnding(w, kazakhLatinEndings, minLatinEndingLength):
		e.kk += endingWeight
		e.weakKK += endingWeight
	default:
		e.en += unknownWordWeight
		e.weakEN += unknownWordWeight
	}
}

// isCommonKazakh — one of the commonKazakhRank most frequent Kazakh words.
func isCommonKazakh(w string) bool {
	rank, ok := kazakhRanks[w]
	return ok && rank <= commonKazakhRank
}

// isEnglishListWord — the word, or its stem before 's / 't / 're …, is in
// the English frequency list.
func isEnglishListWord(w string) bool {
	if englishRanks[w] > 0 {
		return true
	}
	if i := strings.IndexByte(w, '\''); i > 0 && englishSuffixes[w[i+1:]] {
		return englishRanks[w[:i]] > 0
	}
	return false
}

func isUzbekCyrillicMarker(w string) bool {
	if uzbekCyrillicMarkers[w] {
		return true
	}
	for _, prefix := range uzbekCyrillicPrefixes {
		if strings.HasPrefix(w, prefix) {
			return true
		}
	}
	return false
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

// token — one word of a text, lowercased, with ё written as е and every
// apostrophe except the Uzbek ʻ written as '.
type token struct {
	word string
	// original — the word as written.
	original string
	// capitalized — first letter upper case, not the whole word («Айгүл»,
	// not «АЙГҮЛ»).
	capitalized bool
	// initial — the first word of the text or of a sentence (after . ! ? …
	// or a line break, ignoring quotes, brackets, dashes and emoji).
	initial bool
}

// tokenize — the words of a text. An apostrophe inside a word stays in it
// (Uzbek Latin o'zbek, gʻ); everything that is not a letter separates words.
// A word glued to a digit ("S24", "html5", "3D") is dropped. A Latin i inside
// a Cyrillic word is the Kazakh і typed on a Latin layout, as in older Kazakh
// texts («бiз»).
func tokenize(text string) []token {
	var out []token
	runes := []rune(text)
	start := -1
	flush := func(end int) {
		begin := start
		start = -1
		if (begin > 0 && unicode.IsDigit(runes[begin-1])) || (end < len(runes) && unicode.IsDigit(runes[end])) {
			return
		}
		original := runes[begin:end]
		word := normalizeWord(string(original))
		if strings.Contains(word, "i") && isCyrillicExceptI(word) {
			word = strings.ReplaceAll(word, "i", "і")
		}
		out = append(out, token{
			word:        word,
			original:    string(original),
			capitalized: len(original) > 1 && unicode.IsUpper(original[0]) && strings.IndexFunc(string(original[1:]), unicode.IsLower) >= 0,
			initial:     startsSentence(runes, begin),
		})
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

// startsSentence — nothing but spaces, quotes, brackets, dashes and emoji
// between the start of a sentence and runes[at].
func startsSentence(runes []rune, at int) bool {
	for i := at - 1; i >= 0; i-- {
		r := runes[i]
		switch {
		case r == '\n' || strings.ContainsRune(".!?…", r):
			return true
		case unicode.IsSpace(r) || strings.ContainsRune(`«»"“”„'‘’()[]—–-`, r) ||
			unicode.Is(unicode.So, r) || unicode.Is(unicode.Sk, r) || unicode.Is(unicode.Mn, r) || r == '‍':
		default:
			return false
		}
	}
	return true
}

// normalizeWord — lowercase, ё → е, typographic apostrophes → '.
func normalizeWord(word string) string {
	return apostrophes.Replace(strings.ReplaceAll(strings.ToLower(word), "ё", "е"))
}

var apostrophes = strings.NewReplacer("’", "'", "‘", "'", "`", "'", "ʼ", "'")

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
	return r == '\'' || r == '’' || r == '‘' || r == '`' || r == 'ʻ' || r == 'ʼ'
}

// hasUzbekApostrophe — oʻ / gʻ with any apostrophe ("oʻzim", "bo'ladi",
// "g'alaba"), but not an English contraction or possessive ("Who's",
// "Greg's", "Go's"), "o'clock" or an Irish name ("O'Brien").
func hasUzbekApostrophe(t token) bool {
	if t.word == "o'clock" {
		return false
	}
	runes := []rune(t.word)
	original := []rune(t.original)
	for i := 0; i+2 < len(runes); i++ {
		if (runes[i] != 'o' && runes[i] != 'g') || (runes[i+1] != '\'' && runes[i+1] != 'ʻ') {
			continue
		}
		if englishSuffixes[string(runes[i+2:])] {
			continue
		}
		if i == 0 && len(original) == len(runes) && unicode.IsUpper(original[0]) && unicode.IsUpper(original[2]) &&
			strings.IndexFunc(string(original[3:]), unicode.IsLower) >= 0 {
			continue // O'Brien, O'Neill
		}
		return true
	}
	return false
}

// hasBareQ — ағылшын тілінде q-дан кейін әрдайым u тұрады, өзбек пен қазақ
// латынында жоқ.
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
