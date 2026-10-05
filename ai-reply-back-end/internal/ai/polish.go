package ai

import (
	"context"
	"regexp"
	"strings"
	"unicode"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/limits"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// POLISH. While the user types a note for the assistant ("ответь ему вежливо
// я сегодня не могу завтра могу"), the keyboard may ask for a cleaner version
// and offer it as a suggestion. It is a convenience, not a generation: no
// quota is reserved, the tokens are still accounted, and a deterministic guard
// throws away any model output that changed more than spelling and
// punctuation. A rejected or unchanged result is "no suggestion" to the
// client, never an error.

// polishOutputTokens — қысқа жазбаға жетеді; ұзынырақ мәтін ұзындығына қарай алады.
const polishOutputTokens = 200

// PolishRequest — нұсқау өрісіндегі мәтінді түзету сұранысы.
type PolishRequest struct {
	User     domain.User
	DeviceID string
	Text     string
	// InputLanguage — тексерілген пернетақта коды не бос (тек метадерек).
	InputLanguage string
	Platform      string
	AppVersion    string
}

// PolishResult — ұсыныс. Changed=false болса клиент ештеңе көрсетпейді.
type PolishResult struct {
	Text    string
	Changed bool
	// TextLimit — мәтін әкімші бекіткен шектен ұзын болғанда (ErrInstructionTooLong).
	TextLimit int
}

// BuildPolishPrompt — ережелер developer хабарында, жазба <note> ішінде.
func BuildPolishPrompt(text string) Prompt {
	tokens := traits.RuneLen(text)
	if tokens < polishOutputTokens {
		tokens = polishOutputTokens
	}
	if tokens > limits.OutputTokenRange.Max {
		tokens = limits.OutputTokenRange.Max
	}
	return Prompt{
		Developer:       polishInstructions,
		User:            block("note", text),
		MaxOutputTokens: tokens,
		Version:         PromptVersionPolish,
	}
}

// Polish — жазбаның түзетілген нұсқасы. Квотаны жұмсамайды; токендер мен
// usage оқиғасы (mode=polish) жазылады, мәтін — ешқашан.
func (s *Service) Polish(ctx context.Context, req PolishRequest) (PolishResult, error) {
	started := s.clock.Now()
	lim := s.limits.Current(ctx)

	text := strings.TrimSpace(req.Text)
	if text == "" {
		return PolishResult{}, domain.ErrInstructionMissing
	}
	if traits.RuneLen(text) > lim.InstructionChars {
		return PolishResult{TextLimit: lim.InstructionChars}, domain.ErrInstructionTooLong
	}
	entitlement, err := s.subs.Entitlement(ctx, req.User.ID)
	if err != nil {
		return PolishResult{}, err
	}

	c := call{
		Mode:       ModePolish,
		User:       req.User,
		DeviceID:   req.DeviceID,
		Language:   req.InputLanguage,
		Platform:   req.Platform,
		AppVersion: req.AppVersion,
		Chars:      traits.RuneLen(text),
	}
	prompt := BuildPolishPrompt(text)
	completion, err := s.provider.Generate(ctx, prompt)
	latency := int(s.clock.Now().Sub(started).Milliseconds())
	if err != nil {
		s.record(ctx, c, entitlement, "error", errorCode(err), prompt.Version, Completion{}, latency)
		s.logFailure(c, prompt.Version, errorCode(err), latency)
		return PolishResult{}, err
	}

	date, month := s.subs.Keys(started)
	if err := s.repo.AddTokens(ctx, c.User.ID, date, month, completion.InputTokens,
		completion.OutputTokens, s.estimateCostMicros(ctx, completion)); err != nil {
		s.log.Error("token accounting failed", "user_id", c.User.ID, "error", err.Error())
	}
	s.record(ctx, c, entitlement, "success", "", prompt.Version, completion, latency)

	polished, changed := acceptPolish(text, completion)
	s.log.Info("ai_reply_generated", "user_id", c.User.ID, "mode", c.Mode, "prompt_version", prompt.Version,
		"platform", c.Platform, "app_version", c.AppVersion, "latency_ms", latency,
		"input_tokens", completion.InputTokens, "output_tokens", completion.OutputTokens, "changed", changed)
	if !changed {
		return PolishResult{Text: text}, nil
	}
	return PolishResult{Text: polished, Changed: true}, nil
}

// acceptPolish — модель жазбаны тек емле мен тыныс белгісі деңгейінде
// өзгерткенін тексереді. Күмән болса — ұсыныс жоқ.
func acceptPolish(input string, completion Completion) (string, bool) {
	if completion.Truncated {
		return "", false
	}
	out := strings.TrimSpace(completion.Text)
	// Жазбаның өзі тырнақшада болса, тырнақшаны да сақтау керек.
	if UnwrapQuotes(input) == input {
		out = UnwrapQuotes(out)
	}
	switch {
	case out == "" || out == input:
		return "", false
	case strings.Contains(out, "<note") || strings.Contains(out, "</note"):
		return "", false
	case !strings.Contains(input, "\n") && strings.Contains(out, "\n"):
		return "", false
	}
	ratio := float64(traits.RuneLen(out)) / float64(traits.RuneLen(input))
	if ratio < 0.6 || ratio > 1.6 {
		return "", false
	}
	if in, o := Detect(input), Detect(out); in.Confident && o.Confident && in.Lang != o.Lang {
		return "", false
	}
	if !protectedTokensKept(input, out) {
		return "", false
	}
	return out, true
}

var quotedSegment = regexp.MustCompile(`«[^»\n]*»|"[^"\n]*"|“[^”\n]*”`)

// protectedTokensKept — аттар, сандар, сілтемелер, пошта, @/# белгілері және
// тырнақшадағы бөліктер өзгеріссіз қалды ма, ал шығыста жаңа ат не сан
// пайда болмады ма (жазбада жоқ дерек — қосылған ақпарат).
func protectedTokensKept(input, output string) bool {
	for _, segment := range quotedSegment.FindAllString(input, -1) {
		if !strings.Contains(output, segment) {
			return false
		}
	}
	available := tokenCounts(output)
	for _, token := range protectedTokens(input) {
		if available[token] == 0 {
			return false
		}
		available[token]--
	}
	// Регистрді түзету рұқсат («ерлану» → «Ерлану»), жаңа сөз — жоқ.
	known := tokenCounts(strings.ToLower(input))
	for _, token := range protectedTokens(output) {
		if known[strings.ToLower(token)] == 0 {
			return false
		}
	}
	return true
}

// protectedTokens — модель өзгертпеуі тиіс сөздер (регистрімен бірге).
func protectedTokens(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		sentenceStart := true
		for _, raw := range strings.Fields(line) {
			token := trimEdgePunctuation(raw)
			if token != "" && isProtected(token, sentenceStart) {
				out = append(out, token)
			}
			sentenceStart = strings.TrimRight(raw, ".!?…") != raw
		}
	}
	return out
}

func isProtected(token string, sentenceStart bool) bool {
	if hasDigit(token) || strings.ContainsAny(token, "/.@") || strings.HasPrefix(token, "#") {
		return true // сан, сілтеме, домен, пошта, @аты, #тег
	}
	runes := []rune(token)
	for _, r := range runes[1:] {
		if unicode.IsUpper(r) {
			return true // iPhone, WhatsApp, ОК
		}
	}
	return unicode.IsUpper(runes[0]) && !sentenceStart
}

func tokenCounts(text string) map[string]int {
	counts := map[string]int{}
	for _, raw := range strings.Fields(text) {
		if token := trimEdgePunctuation(raw); token != "" {
			counts[token]++
		}
	}
	return counts
}

// trimEdgePunctuation — сөз шетіндегі тыныс белгілері (@, # және + сақталады).
func trimEdgePunctuation(token string) string {
	return strings.TrimFunc(token, func(r rune) bool {
		return strings.ContainsRune(`.,!?;:…()[]{}«»"“”„'’—–-`, r)
	})
}

func hasDigit(s string) bool {
	return strings.IndexFunc(s, unicode.IsDigit) >= 0
}
