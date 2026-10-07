package ai

import (
	"context"
	"strings"

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
	// AppLanguage — интерфейс тілі. Нұсқаудың да, пернетақтаның да тілі анық
	// болмаса ғана (атаулар, эмодзи) шешуші болады; тексерілген код қана қолданылады.
	AppLanguage string
	// InputLanguage — «Create» басылғандағы пернетақта (kk | ru | en).
	InputLanguage string
	// GrammaticalGender — male | female | unspecified.
	GrammaticalGender string
	// Regenerate — пайдаланушы бір нұсқаны көріп, басқасын сұрады.
	Regenerate bool
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
	sections = append(sections, composeOutput)

	user := "Write the message described below. The block is the user's request.\n\n" +
		block("request", instruction) + "\n\nWrite only the message."
	return Prompt{
		Developer: strings.Join(sections, "\n\n"),
		User:      user,
		Version:   PromptVersionCompose,
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
	User        domain.User
	DeviceID    string
	Instruction string
	Language    string
	// InputLanguage — тексерілген пернетақта коды не бос.
	InputLanguage string
	// GrammaticalGender — сұраныстағы мән, әйтпесе сақталған профильдегі.
	GrammaticalGender string
	Regenerate        bool
	Platform          string
	AppVersion        string
}

// Compose — нұсқау бойынша жаңа хабарлама.
//
// Same guarantees as Reply: the quota is reserved before the provider call and
// refunded if it fails, and neither the instruction nor the message is stored
// or logged — the usage event carries the instruction's length only.
func (s *Service) Compose(ctx context.Context, req ComposeRequest) (Result, error) {
	started := s.clock.Now()
	lim := s.limits.Current(ctx)

	instruction := strings.TrimSpace(req.Instruction)
	if instruction == "" {
		return Result{}, domain.ErrInstructionMissing
	}
	// Rejected, not clamped: the instruction is the whole request here, and
	// cutting it would write a message about something the user did not say.
	if traits.RuneLen(instruction) > lim.InstructionChars {
		return Result{InstructionLimit: lim.InstructionChars}, domain.ErrInstructionTooLong
	}

	prompt := BuildComposePrompt(ComposeInput{
		Instruction:       instruction,
		AppLanguage:       req.Language,
		InputLanguage:     req.InputLanguage,
		GrammaticalGender: req.GrammaticalGender,
		Regenerate:        req.Regenerate,
	})
	prompt.MaxOutputTokens = ComposeTokens(lim.MaxOutputTokens)

	return s.generate(ctx, call{
		Mode:       ModeCompose,
		User:       req.User,
		DeviceID:   req.DeviceID,
		Language:   req.Language,
		Platform:   req.Platform,
		AppVersion: req.AppVersion,
		Chars:      traits.RuneLen(instruction),
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
