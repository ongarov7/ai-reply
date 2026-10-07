package eval

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/ai"
)

// TestLiveEval — нақты модельмен бағалау. Өндірістегі промпт құрастырушылар,
// тазарту, тексеру және түзету қолданылады; нәтиже кестесі тест журналына
// шығады, файлға ештеңе жазылмайды.
//
//	AIREPLY_LIVE_EVAL=1 OPENAI_API_KEY=sk-… go test ./internal/ai/eval -run TestLiveEval -count=1 -v
//
// The model is stochastic: read the table, not just the exit code, and run it
// more than once before trusting a single pass or fail.
func TestLiveEval(t *testing.T) {
	if os.Getenv("AIREPLY_LIVE_EVAL") != "1" || os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("set AIREPLY_LIVE_EVAL=1 and OPENAI_API_KEY to call the real model")
	}
	cases, err := Cases()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.OpenAIFromEnv()
	provider := ai.NewOpenAI(cfg)

	var table strings.Builder
	fmt.Fprintf(&table, "model %s\n%-34s %-6s %-20s %s\n", cfg.Model, "case", "result", "prompt", "output / failures")
	passed := 0
	for _, c := range cases {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		out, err := ai.Complete(ctx, provider, livePrompt(c, cfg.MaxOutputTokens), true)
		cancel()
		if err != nil {
			t.Errorf("%s: %v", c.ID, err)
			fmt.Fprintf(&table, "%-34s %-6s %-20s %v\n", c.ID, "ERROR", "", err)
			continue
		}
		failures := Evaluate(c, out.Text)
		result := "PASS"
		if len(failures) > 0 {
			result = "FAIL"
			t.Errorf("%s: %v\n  output: %s", c.ID, failures, out.Text)
		} else {
			passed++
		}
		fmt.Fprintf(&table, "%-34s %-6s %-20s %s\n", c.ID, result, out.Version, strings.ReplaceAll(out.Text, "\n", " ⏎ "))
		for _, f := range failures {
			fmt.Fprintf(&table, "%62s %s\n", "", f)
		}
	}
	fmt.Fprintf(&table, "passed %d of %d", passed, len(cases))
	t.Log("\n" + table.String())
}

// livePrompt — клиент жіберетін сұранысқа сай промпт. Шаблон әдепкілері
// қолданбалардағы қарым-қатынас түрлерімен бірдей.
func livePrompt(c Case, maxOutputTokens int) ai.Prompt {
	if c.Mode == "compose" {
		prompt := ai.BuildComposePrompt(ai.ComposeInput{Instruction: c.Instruction, AppLanguage: c.appLanguage(),
			InputLanguage: c.InputLanguage, GrammaticalGender: c.Gender})
		prompt.MaxOutputTokens = ai.ComposeTokens(maxOutputTokens)
		return prompt
	}
	template := ai.Template{Relationship: c.TemplateID, Tone: "professional", ReplyLength: "short",
		EmojiPolicy: "minimal", WorkingHoursBehavior: "mention_when_relevant"}
	switch c.TemplateID {
	case "friend":
		template.Tone, template.EmojiPolicy, template.WorkingHoursBehavior = "friendly", "allowed", "ignore"
	case "business":
		template.EmojiPolicy = "none"
	case "work":
		template.Tone = "natural"
	}
	prompt := ai.BuildPrompt(ai.PromptInput{Message: c.Incoming, Instruction: c.Instruction, TemplateID: c.TemplateID,
		AppLanguage: c.appLanguage(), InputLanguage: c.InputLanguage,
		Profile: ai.Profile{PreferredTone: template.Tone, GrammaticalGender: c.Gender}, Template: template})
	prompt.MaxOutputTokens = maxOutputTokens
	return prompt
}
