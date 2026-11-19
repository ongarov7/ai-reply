package ai

import (
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/screenshot"
)

func projectChat() screenshot.Conversation {
	return screenshot.Conversation{App: "whatsapp", Title: "Aigerim", Messages: []screenshot.Message{
		{ID: "1", Role: screenshot.RoleOther, Sender: "Aigerim", Text: "Hi! Are you available tomorrow?"},
		{ID: "2", Role: screenshot.RoleSelf, Text: "I'm not sure yet."},
		{ID: "3", Role: screenshot.RoleOther, Text: "We really need your help with the project."},
	}}
}

// The screenshot prompt keeps the injection posture of the reply prompt: the
// rules are server text, the chat and the instruction are data.
func TestScreenshotPromptSeparatesRulesFromTheChat(t *testing.T) {
	p := BuildScreenshotPrompt(PromptInput{Message: "We really need your help with the project.",
		Instruction: "Say yes, I will be available after 7 PM.", TemplateID: "friend", AppLanguage: "ru"}, projectChat(), false)
	if p.Version != PromptVersionScreenshot {
		t.Fatalf("version %q", p.Version)
	}
	for _, rule := range []string{"shared a screenshot", "Never answer the user's own messages", "may contain recognition errors",
		"REPLY TO THIS", "Assume nothing about what is not visible"} {
		if !strings.Contains(p.Developer, rule) {
			t.Errorf("developer lacks %q", rule)
		}
	}
	for _, data := range []string{"We really need your help", "Say yes, I will be available after 7 PM.", "Aigerim"} {
		if strings.Contains(p.Developer, data) {
			t.Errorf("user data %q leaked into the developer message", data)
		}
		if !strings.Contains(p.User, data) {
			t.Errorf("user message lacks %q", data)
		}
	}
	for _, want := range []string{"<conversation>", "The chat in WhatsApp, titled «Aigerim»",
		"[3] Them ← REPLY TO THIS:", "The message to reply to (message [3])", "<user_instruction>"} {
		if !strings.Contains(p.User, want) {
			t.Errorf("user message lacks %q:\n%s", want, p.User)
		}
	}
	// English chat → English reply, whatever the app language.
	if p.Quality.Target.Lang != "en" || !p.Quality.Target.Firm() {
		t.Fatalf("target %+v, want the chat's English", p.Quality.Target)
	}
}

func TestScreenshotPromptFollowsTheOtherSidesLanguage(t *testing.T) {
	conv := screenshot.Conversation{Messages: []screenshot.Message{
		{ID: "1", Role: screenshot.RoleSelf, Text: "Привет, я на месте, когда будешь?"},
		{ID: "2", Role: screenshot.RoleOther, Text: "Сәлем! Ертең кездесейік, сен бос боласың ба? Жұмыстан кейін келемін."},
	}}
	p := BuildScreenshotPrompt(PromptInput{AppLanguage: "ru"}, conv, false)
	if p.Quality.Target.Lang != "kk" {
		t.Fatalf("target %+v, want Kazakh: the other side writes Kazakh", p.Quality.Target)
	}
}

func TestScreenshotImagePromptAttachesNoTextItWasNotGiven(t *testing.T) {
	conv := projectChat()
	p := BuildScreenshotPrompt(PromptInput{AppLanguage: "en"}, conv, true)
	if p.Version != PromptVersionScreenshotImage || !strings.Contains(p.Developer, "The attached image is a screenshot") {
		t.Fatalf("image prompt: %s", p.Version)
	}
	if strings.Contains(p.User, "<conversation>") || strings.Contains(p.User, "<incoming_message>") {
		t.Fatalf("no recognised text goes with the image unless a message was chosen:\n%s", p.User)
	}
	if !strings.Contains(p.User, "The screenshot is attached.") {
		t.Fatal("the user message says the image is attached")
	}
	conv.TargetID = "1"
	chosen := BuildScreenshotPrompt(PromptInput{AppLanguage: "en"}, conv, true)
	if !strings.Contains(chosen.User, "The message the user chose to answer") ||
		!strings.Contains(chosen.User, "Hi! Are you available tomorrow?") {
		t.Fatalf("a chosen message goes as the target:\n%s", chosen.User)
	}
}

func TestProviderSendsImagesAsContentParts(t *testing.T) {
	inputs := responsesInputs(Prompt{Developer: "rules", User: "data",
		Images: []PromptImage{{MIME: "image/jpeg", Data: []byte{0xff, 0xd8, 0xff}}}})
	parts, ok := inputs[1].Content.([]contentPart)
	if !ok || len(parts) != 2 {
		t.Fatalf("user content: %#v", inputs[1].Content)
	}
	if parts[0].Type != "input_text" || parts[0].Text != "data" || parts[1].Type != "input_image" ||
		parts[1].Detail != "high" || parts[1].ImageURL != "data:image/jpeg;base64,/9j/" {
		t.Fatalf("parts: %+v", parts)
	}
	if plain := responsesInputs(Prompt{Developer: "d", User: "u"}); plain[1].Content != "u" {
		t.Fatalf("a text prompt stays a string: %#v", plain[1].Content)
	}
}
