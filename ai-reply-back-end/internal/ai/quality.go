package ai

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// OUTPUT QUALITY. Every reply and compose result goes through the same steps:
// a deterministic cleanup (CleanOutput), a check for the mistakes users notice
// most (Check: the sender's grammatical gender in Russian, the wrong language,
// a label or a preamble left in the text) and, only when the check finds
// something, one repair call. The repaired text is used only if it passes the
// same check; otherwise the cleaned original goes out. Nothing here logs or
// stores text.

// Issue — жауаптағы мәселенің коды (журналға тек код жазылады).
type Issue string

// Мәселе түрлері.
const (
	IssueGender   Issue = "gender"
	IssueLanguage Issue = "language"
	IssueMeta     Issue = "meta"
)

var (
	// labelPrefix — «Ответ:», «Жауап:», "Reply:" … мәтін басында.
	labelPrefix = regexp.MustCompile(`(?i)^\s*(?:ответ|вариант ответа|сообщение|жауап|хабарлама|javob|reply|response|answer|message)\s*:\s*`)
	// preamblePrefix — «Конечно! Вот вариант ответа:», "Sure! Here is a reply:".
	// Тек жауапқа қатысты сөз болса ғана: «Вот мой номер: …» — хабардың өзі.
	preamblePrefix = regexp.MustCompile(`(?i)^\s*(?:(?:конечно|хорошо|разумеется|әрине|жарайды|sure|of course|certainly|okay)[!.,]?\s*)?` +
		`(?:вот|мінеки|міне|here's|here’s|here is|here are|here)[^\p{L}:\n][^:\n]{0,40}?` +
		`(?:ответ|вариант|сообщени|текст|поздравлени|жауап|хабарлама|нұсқа|reply|response|answer|message|version|option|draft|text)` +
		`[^:\n]{0,30}:\s*`)
	// trailingNote — «Примечание: …», "Note: …" соңғы абзацта.
	trailingNote = regexp.MustCompile(`(?i)^\(?\s*(?:примечание|note|ескерту|ескертпе)\s*[:.—–-]`)
	// alternatives — «Вариант 1», "Option 2" жол басында.
	alternatives = regexp.MustCompile(`(?im)^\s*(?:вариант|option|нұсқа)\s*\d`)
	// promptTags — промпт блоктарының аттары жауапқа түспеуі керек.
	promptTags = regexp.MustCompile(`</?(?:incoming_message|user_instruction|user_profile|request|note|message)>`)

	markdownBold     = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	markdownUnder    = regexp.MustCompile(`__([^_\n]+)__`)
	markdownHeading  = regexp.MustCompile(`(?m)^#{1,6}[ \t]+`)
	trailingSpaces   = regexp.MustCompile(`(?m)[ \t]+$`)
	extraBlankLines  = regexp.MustCompile(`\n{3,}`)
	paragraphBreak   = regexp.MustCompile(`\n[ \t]*\n`)
	hasLetterOrDigit = regexp.MustCompile(`[\p{L}\p{N}]`)
)

// CleanOutput — модель жауабын детерминді тазарту: тырнақша, жапсырма,
// кіріспе, markdown, соңғы ескерту, артық бос жолдар. Қалыпты мәтіннің
// ортасына тимейді.
func CleanOutput(text string) string {
	original := strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	out := UnwrapQuotes(original)

	for _, prefix := range []*regexp.Regexp{preamblePrefix, labelPrefix} {
		if loc := prefix.FindStringIndex(out); loc != nil && hasLetterOrDigit.MatchString(out[loc[1]:]) {
			out = UnwrapQuotes(strings.TrimSpace(out[loc[1]:]))
		}
	}

	out = markdownBold.ReplaceAllString(out, "$1")
	out = markdownUnder.ReplaceAllString(out, "$1")
	out = markdownHeading.ReplaceAllString(out, "")

	if paragraphs := paragraphBreak.Split(out, -1); len(paragraphs) > 1 &&
		trailingNote.MatchString(strings.TrimSpace(paragraphs[len(paragraphs)-1])) {
		out = strings.Join(paragraphs[:len(paragraphs)-1], "\n\n")
	}

	out = trailingSpaces.ReplaceAllString(out, "")
	out = extraBlankLines.ReplaceAllString(out, "\n\n")
	out = UnwrapQuotes(strings.TrimSpace(out))
	if out == "" {
		return original
	}
	return out
}

// UnwrapQuotes — модель кейде бүкіл жауапты тырнақшаға алады; соны ғана алып тастаймыз.
func UnwrapQuotes(text string) string {
	pairs := [][2]string{{`"`, `"`}, {"“", "”"}, {"«", "»"}}
	for _, pair := range pairs {
		if len([]rune(text)) > 2 && strings.HasPrefix(text, pair[0]) && strings.HasSuffix(text, pair[1]) {
			inner := strings.TrimSuffix(strings.TrimPrefix(text, pair[0]), pair[1])
			if !strings.Contains(inner, pair[1]) {
				return strings.TrimSpace(inner)
			}
		}
	}
	return text
}

// Check — тазартылған жауапты тексеру. target бос болса тіл тексерілмейді;
// gender unspecified болса жыныс тексерілмейді.
func Check(text, target, gender string) []Issue {
	var issues []Issue
	masculine, feminine := GenderedSelfForms(text)
	if (gender == domain.GenderMale && feminine) || (gender == domain.GenderFemale && masculine) {
		issues = append(issues, IssueGender)
	}
	if InputLanguages[target] {
		if d := Detect(text); d.Confident && d.Lang != target {
			issues = append(issues, IssueLanguage)
		}
	}
	if hasMeta(text) {
		issues = append(issues, IssueMeta)
	}
	return issues
}

func hasMeta(text string) bool {
	if labelPrefix.MatchString(text) || preamblePrefix.MatchString(text) ||
		alternatives.MatchString(text) || promptTags.MatchString(text) {
		return true
	}
	paragraphs := paragraphBreak.Split(text, -1)
	for _, p := range paragraphs[1:] {
		if trailingNote.MatchString(strings.TrimSpace(p)) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- gender

// RE2 has no Unicode \b, so word edges are spelled out. A first-person form
// is «я» followed by up to three short fillers and a past-tense verb or a
// short adjective; «буду» plus a short adjective; or a short adjective at the
// start of a sentence or clause, after at most two fillers («Рада была
// помочь», «Всегда рад»). The fillers are a closed list on purpose: «я думаю
// она пришла» must not count as the sender's form. A form followed by «?»
// («Готова?») is a question to the other person and does not count either.
const (
	selfFillers = `(?:уже|тоже|также|так|очень|бы|не|ни|и|ещё|еще|всё|все|вчера|сегодня|сейчас|просто|правда|` +
		`правильно|точно|всегда|давно|только|сразу|вот|же|ведь|уж|опять|снова|тогда|` +
		`тебе|тебя|вам|вас|ему|ей|им|их|его|её|ее|нам|нас)`
	wordStart   = `(?:^|[^\p{L}])`
	wordEnd     = `(?:$|[^\p{L}?])`
	clauseStart = `(?:^|[.!?…,—–-]\s*|\n\s*)`
)

var (
	feminineSelf  = selfForms(`(?:ла|лась)`, `была`, `(?:рада|готова|занята|согласна|уверена|свободна|должна|благодарна)`)
	masculineSelf = selfForms(`(?:л|лся)`, `был`, `(?:рад|готов|занят|согласен|уверен|свободен|должен|благодарен)`)
)

func selfForms(pastEnding, was, adjectives string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)` +
		wordStart + `я(?:\s+` + selfFillers + `){0,3}\s+(?:\p{L}+` + pastEnding + `|` + adjectives + `)` + wordEnd +
		`|` + wordStart + `буду(?:\s+` + selfFillers + `){0,2}\s+` + adjectives + wordEnd +
		`|` + clauseStart + `(?:` + selfFillers + `\s+){0,2}(?:` + was + `\s+|буду\s+)?` + adjectives + wordEnd)
}

// GenderedSelfForms — орысша мәтінде жіберуші өзі туралы ер не әйел
// тегіндегі тұлғаны қолданды ма («я сделал», «рада помочь»). Алушыға
// қатысты тұлғалар («ты пришла») есептелмейді.
func GenderedSelfForms(text string) (masculine, feminine bool) {
	return masculineSelf.MatchString(text), feminineSelf.MatchString(text)
}

// ---------------------------------------------------------------- truncation

// trimToSentence — токен шегінде үзілген мәтінді соңғы толық сөйлемге дейін
// қысқартады, егер ол мәтіннің екінші жартысында болса; әйтпесе өзгертпейді.
func trimToSentence(text string) string {
	runes := []rune(strings.TrimSpace(text))
	for i := len(runes) - 1; i >= len(runes)/2 && i > 0; i-- {
		r := runes[i]
		end := strings.ContainsRune("!?…\n", r) ||
			(r == '.' && (i+1 == len(runes) || unicode.IsSpace(runes[i+1]) || strings.ContainsRune(`»"”)`, runes[i+1])))
		if !end {
			continue
		}
		cut := i + 1
		for cut < len(runes) && strings.ContainsRune(`»"”)`, runes[cut]) {
			cut++
		}
		return strings.TrimSpace(string(runes[:cut]))
	}
	return string(runes)
}

// ---------------------------------------------------------------- repair

// BuildRepairPrompt — бір хабарды түзету: developer хабарында тек серверлік
// мәселе сөйлемдері, хабардың өзі user хабарында <message> ішінде.
func BuildRepairPrompt(text string, issues []Issue, q Quality) Prompt {
	var problems []string
	for _, issue := range issues {
		problems = append(problems, "- "+repairProblem(issue, q))
	}
	developer := repairInstructions + "\n\nPROBLEMS\n" + strings.Join(problems, "\n")
	// A rewrite into another language gets that language's quality rules, so
	// it reads as written in it rather than translated.
	if rules, ok := languageRules[q.Target.Lang]; ok && slices.Contains(issues, IssueLanguage) {
		developer += "\n\nLANGUAGE\n" + rules
	}
	developer += "\n\nSENDER\n" + repairSenderNotes[senderGender(q.Gender)]
	return Prompt{
		Developer: developer,
		User:      block("message", text),
		Version:   PromptVersionRepair,
		Quality:   q,
	}
}

// Түзету клиенттің күтуіне (iOS — 25 с) сыюы керек: бірінші жауап пен түзету
// бірге repairDeadline-нан аспайды, ал minRepairWindow-дан аз уақыт қалса,
// түзету басталмайды.
const (
	repairDeadline  = 18 * time.Second
	minRepairWindow = 4 * time.Second
)

// Outcome — бір генерацияның нәтижесі: тазартылған мәтін және оның есебі.
type Outcome struct {
	Text         string
	Model        string
	InputTokens  int
	OutputTokens int
	ProviderMS   int
	// Version — промпт нұсқасы; түзету қабылданса "+repair_v1" қосылады.
	Version string
	// Issues — бірінші жауапта табылған мәселелер (түзетуге дейін).
	Issues []Issue
	// RepairAttempted — түзету сұралды; Repaired — түзетілген мәтін қабылданды.
	RepairAttempted bool
	Repaired        bool
	// RepairError — түзету сұранысының қате коды; бірінші жауап қалады.
	RepairError string
	// Truncated — провайдер жауапты токен шегінде үзді.
	Truncated bool
}

// Complete — провайдер → тазарту → тексеру → қажет болса бір түзету.
// Квота мен есеп мұнда жоқ: оны Service.generate жүргізеді, ал сапа
// бағалауының тірі тесті осы жолды тікелей шақырады.
func Complete(ctx context.Context, provider Provider, prompt Prompt, repair bool) (Outcome, error) {
	started := time.Now()
	first, err := provider.Generate(ctx, prompt)
	if err != nil {
		return Outcome{}, err
	}
	out := Outcome{
		Text:         CleanOutput(first.Text),
		Model:        first.Model,
		InputTokens:  first.InputTokens,
		OutputTokens: first.OutputTokens,
		ProviderMS:   first.ProviderMS,
		Version:      prompt.Version,
		Truncated:    first.Truncated,
	}
	target, gender := prompt.Quality.verifiedLanguage(), prompt.Quality.Gender
	out.Issues = Check(out.Text, target, gender)
	if len(out.Issues) == 0 || !repair {
		return out, nil
	}
	remaining := repairDeadline - time.Since(started)
	if remaining < minRepairWindow {
		return out, nil
	}

	repairCtx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	fix := BuildRepairPrompt(out.Text, out.Issues, prompt.Quality)
	fix.MaxOutputTokens = prompt.MaxOutputTokens
	out.RepairAttempted = true
	second, err := provider.Generate(repairCtx, fix)
	if err != nil {
		out.RepairError = errorCode(err)
		return out, nil
	}
	out.InputTokens += second.InputTokens
	out.OutputTokens += second.OutputTokens
	out.ProviderMS += second.ProviderMS
	if fixed := CleanOutput(second.Text); !second.Truncated && len(Check(fixed, target, gender)) == 0 {
		out.Text = fixed
		out.Repaired = true
		out.Version += "+" + PromptVersionRepair
	}
	return out, nil
}

// issueCodes — журналға арналған кодтар тізімі.
func issueCodes(issues []Issue) []string {
	out := make([]string, 0, len(issues))
	for _, issue := range issues {
		out = append(out, string(issue))
	}
	return out
}
