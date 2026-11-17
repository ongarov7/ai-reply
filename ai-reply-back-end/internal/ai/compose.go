package ai

import (
	"context"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/aicontext"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/limits"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// COMPOSE MODE. The user describes a message ("поздравь директора с 55-летием")
// and the model writes it. There is no incoming message: nothing from the
// clipboard is involved, and the reply rules (answer the incoming message,
// match its length, never greet first) do not apply. It shares the quota, the
// provider, the output checks and the usage accounting with Reply; only the
// prompt is its own.
//
// The request is the user's own instruction, so here it legitimately decides
// what gets written. It still lives in the USER message, inside <request>:
// it can shape the message, not the rules about what the output is.

// composeOutputTokens — a congratulation in Kazakh or Russian runs to a few
// hundred tokens, more than the reply cap allows (180 by default). Compose
// gets this floor; an administrator who set a higher cap keeps theirs.
const composeOutputTokens = 700

// ComposeInput — compose промптына керек дерек.
type ComposeInput struct {
	Instruction string
	TemplateID  string
	// AppLanguage — интерфейс тілі. Нұсқаудың да, пернетақтаның да тілі анық
	// болмаса ғана (атаулар, эмодзи) шешуші болады; тексерілген код қана қолданылады.
	AppLanguage string
	// InputLanguage — «Create» басылғандағы пернетақта (kk | ru | en).
	InputLanguage string
	// GrammaticalGender — male | female | unspecified.
	GrammaticalGender string
	// Regenerate — пайдаланушы бір нұсқаны көріп, басқасын сұрады.
	Regenerate bool
	// Account — the AI context saved in the user's account; empty when none.
	Account aicontext.Rendered
}

// BuildComposePrompt — compose ережелері developer хабарында, пайдаланушы
// сұранысы user хабарында, <request> блогында.
func BuildComposePrompt(in ComposeInput) Prompt {
	instruction := strings.TrimSpace(in.Instruction)
	target := ResolveComposeTarget(instruction, in.InputLanguage, in.AppLanguage)
	gender := senderGender(in.GrammaticalGender)
	namesLanguage := MentionsLanguage(instruction)

	sections := []string{
		composeRole,
		composeProductRules,
		languageSection(composeLanguageLead(target), target.Lang, !target.Firm() || namesLanguage),
		senderSection(gender, "the request"),
		composeStyle,
	}
	if in.Regenerate {
		sections = append(sections, composeAnotherVersion)
	}
	if in.TemplateID != "" {
		sections = append(sections, "RECIPIENT\n"+relationshipHint(in.TemplateID))
	}
	sections = append(sections, composeOutput)

	user := "Write the message described below. The block is the user's request.\n\n" +
		block("request", instruction) + "\n\nWrite only the message."
	version := PromptVersionCompose
	if account := accountBlocks(in.Account, true); len(account) > 0 {
		user = "Write the message described below. The <request> block is the user's request; " +
			"the blocks before it are background data about the user.\n\n" +
			strings.Join(account, "\n\n") + "\n\n" + block("request", instruction) + "\n\nWrite only the message."
		version += PromptSuffixAccountContext
	}
	return Prompt{
		Developer: strings.Join(sections, "\n\n"),
		User:      user,
		Version:   version,
		Quality: Quality{
			Target:         target,
			Gender:         gender,
			VerifyLanguage: target.Firm() && !namesLanguage,
			Compose:        true,
		},
	}
}

// ComposeRequest — «Create» батырмасынан келген бір сұраныс.
type ComposeRequest struct {
	User            domain.User
	DeviceID        string
	SessionPlatform string
	Context         *GenerationContext
	OutputCharLimit int
	TemplateID      string
	Instruction     string
	Language        string
	// InputLanguage — тексерілген пернетақта коды не бос.
	InputLanguage string
	// GrammaticalGender — сұраныстағы мән, әйтпесе сақталған профильдегі.
	GrammaticalGender string
	Regenerate        bool
	Platform          string
	AppVersion        string
	// IdempotencyKey — Idempotency-Key тақырыбы ("" — кілтсіз): бір әрекетке бір төлем.
	IdempotencyKey string
	// Account and AccountExtra — the account AI context for this request and
	// the extra replies it costs on a limited plan (docs/web-cabinet-ai-context.md §3).
	Account      aicontext.Rendered
	AccountExtra int
}

// Compose — нұсқау бойынша жаңа хабарлама.
//
// Same guarantees as Reply: the quota is reserved before the provider call and
// refunded if it fails, and neither the instruction nor the message is stored
// or logged — the usage event carries the instruction's length only.
func (s *Service) Compose(ctx context.Context, req ComposeRequest) (Result, error) {
	started := s.clock.Now()
	lim := s.limits.Current(ctx)
	mac, err := s.validateMacGeneration(req.SessionPlatform, req.Context, req.OutputCharLimit, req.TemplateID)
	if err != nil {
		return Result{}, err
	}

	instruction := strings.TrimSpace(req.Instruction)
	if instruction == "" {
		return Result{}, domain.ErrInstructionMissing
	}
	// Rejected, not clamped: the instruction is the whole request here, and
	// cutting it would write a message about something the user did not say.
	if traits.RuneLen(instruction) > lim.InstructionChars {
		return Result{InstructionLimit: lim.InstructionChars}, domain.ErrInstructionTooLong
	}

	input := ComposeInput{
		Instruction:       instruction,
		AppLanguage:       req.Language,
		InputLanguage:     req.InputLanguage,
		GrammaticalGender: req.GrammaticalGender,
		Regenerate:        req.Regenerate,
		TemplateID:        req.TemplateID,
	}
	base := BuildComposePrompt(input)
	base.MaxOutputTokens = ComposeTokens(lim.MaxOutputTokens)
	var full Prompt
	if req.Account.Present() {
		input.Account = req.Account
		full = BuildComposePrompt(input)
		full.MaxOutputTokens = base.MaxOutputTokens
	}
	prompt, fallback, err := s.withAccount(base, full, req.Account.Present(), mac, req.Context, req.OutputCharLimit)
	if err != nil {
		return Result{}, err
	}

	return s.generate(ctx, call{
		Mode:         ModeCompose,
		User:         req.User,
		DeviceID:     req.DeviceID,
		Language:     req.Language,
		Platform:     req.Platform,
		AppVersion:   req.AppVersion,
		Chars:        traits.RuneLen(instruction),
		Key:          req.IdempotencyKey,
		Fallback:     fallback,
		Extra:        req.AccountExtra,
		ContextChars: req.Account.Chars,
	}, prompt, started)
}

// ComposeTokens — compose жауабының токен шегі: кемінде composeOutputTokens,
// әкімші одан көп қойса — соның өзі, бірақ OutputTokenRange.Max-тан аспайды.
func ComposeTokens(adminCap int) int {
	tokens := composeOutputTokens
	if adminCap > tokens {
		tokens = adminCap
	}
	if tokens > limits.OutputTokenRange.Max {
		tokens = limits.OutputTokenRange.Max
	}
	return tokens
}
