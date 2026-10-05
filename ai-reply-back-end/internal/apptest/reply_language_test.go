package apptest

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// CUSTOMER REQUIREMENT: the copied message decides the reply language. These
// requests come from a phone whose app language and keyboard are Russian and
// carry a Russian quick action, the way the keyboards send them.

const russianQuickAction = "Ответь согласием."

func replyBody(message string, extra map[string]any) map[string]any {
	body := map[string]any{"source_text": message, "instruction": russianQuickAction, "language": "ru",
		"input_language": "ru", "template_id": "friend", "platform": "ios", "app_version": "2.0.0"}
	for key, value := range extra {
		body[key] = value
	}
	return body
}

func TestKazakhMessageGetsAKazakhReplyOnARussianPhone(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 707 610 20 01")
	for name, message := range map[string]string{
		"kazakh letters":             "Ертең кездесуге уақытың бар ма?",
		"kazakh without its letters": "Калайсын? Ертен кездесемиз бе?",
		"short":                      "Кайдасын?",
		"short thanks":               "Рахмет!",
		"mixed with russian":         "Сәлем, как дела? Ертең келесің бе?",
	} {
		res := h.do(http.MethodPost, "/api/v1/ai/reply", replyBody(message, nil), h.auth(session.access))
		if res.status != http.StatusOK {
			t.Fatalf("%s: %d %s", name, res.status, res.raw)
		}
		if !strings.Contains(h.provider.lastDeveloper, "Write the reply in Kazakh, the language of the incoming message.") {
			t.Errorf("%s: the developer message does not target Kazakh", name)
		}
		if strings.Contains(h.provider.lastDeveloper, russianQuickAction) ||
			!strings.Contains(h.provider.lastUser, "<user_instruction>") || !strings.Contains(h.provider.lastUser, russianQuickAction) {
			t.Errorf("%s: the quick action must stay data in the user message", name)
		}
	}
	if !strings.Contains(h.logs.String(), `"target_language":"kk","language_source":"message"`) {
		t.Fatal("the success log does not show the Kazakh target taken from the message")
	}
}

func TestOtherMessagesKeepTheirLanguageWhateverThePhone(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 707 610 20 02")
	cases := []struct {
		message, app, keyboard, instruction, want string
	}{
		{"Привет! Ты сегодня придёшь?", "kk", "kk", "Келісетінімді айт", "Write the reply in Russian, the language of the incoming message."},
		{"Hey, are you coming tonight?", "ru", "ru", "Согласись", "Write the reply in English, the language of the incoming message."},
	}
	for _, c := range cases {
		res := h.do(http.MethodPost, "/api/v1/ai/reply", replyBody(c.message, map[string]any{
			"language": c.app, "input_language": c.keyboard, "instruction": c.instruction}), h.auth(session.access))
		if res.status != http.StatusOK || !strings.Contains(h.provider.lastDeveloper, c.want) {
			t.Fatalf("%q: %d, developer message lacks %q", c.message, res.status, c.want)
		}
	}

	// The saved preference still wins over the message.
	res := h.do(http.MethodPost, "/api/v1/ai/reply", replyBody("Ертең кездесуге уақытың бар ма?", map[string]any{
		"profile": map[string]any{"reply_language": "ru"}}), h.auth(session.access))
	if res.status != http.StatusOK || !strings.Contains(h.provider.lastDeveloper, "The user always wants replies in Russian") {
		t.Fatalf("preference: %d %s", res.status, res.raw)
	}
}

// The model answered a Kazakh message in Russian (it followed the Russian
// quick action): the single repair call rewrites the reply into Kazakh.
func TestReplyRepairRewritesARussianAnswerIntoKazakh(t *testing.T) {
	h := newHarness(t, withEnv("AI_REPAIR_ENABLED", "true"))
	session := h.signIn("+7 707 610 20 03")
	russian, kazakh := "Да, конечно, давай встретимся!", "Иә, әрине, кездесейік!"
	h.provider.replies = []string{russian, kazakh}

	res := h.do(http.MethodPost, "/api/v1/ai/reply", replyBody("Ертең кездесуге уақытың бар ма?", nil), h.auth(session.access))
	if res.status != http.StatusOK || res.str("reply") != kazakh || res.str("detected_language") != "kk" {
		t.Fatalf("reply: %d %s", res.status, res.raw)
	}
	if h.provider.calls != 2 || res.num("usage", "used_today") != 1 {
		t.Fatalf("calls %d, usage %s", h.provider.calls, res.raw)
	}
	repair := h.provider.prompts[1]
	if repair.Version != "repair_v1" || !strings.Contains(repair.Developer, "The message must be in Kazakh") ||
		!strings.Contains(repair.User, russian) {
		t.Fatalf("repair prompt: %+v", repair)
	}
	events, _ := h.store.UserEvents(context.Background(), session.userID, 10)
	if len(events) != 1 || events[0].PromptVersion != "reply_v2+repair_v1" {
		t.Fatalf("events: %+v", events)
	}
	logs := h.logs.String()
	if !strings.Contains(logs, `"issues":["language"]`) {
		t.Fatal("the language repair is not logged with its issue code")
	}
	for _, text := range []string{russian, kazakh, "Ертең кездесуге", russianQuickAction} {
		if strings.Contains(logs, text) || h.dbContains(text) {
			t.Fatalf("%q was written somewhere", text)
		}
	}
}

// "Ответь на русском" is an explicit request: the Russian answer stays.
func TestExplicitLanguageRequestIsNotRepaired(t *testing.T) {
	h := newHarness(t, withEnv("AI_REPAIR_ENABLED", "true"))
	session := h.signIn("+7 707 610 20 04")
	h.provider.replies = []string{"Да, приду!"}

	res := h.do(http.MethodPost, "/api/v1/ai/reply", replyBody("Ертең кездесуге уақытың бар ма?", map[string]any{
		"instruction": "Ответь на русском, что приду", "language": "kk", "input_language": "kk"}), h.auth(session.access))
	if res.status != http.StatusOK || res.str("reply") != "Да, приду!" || h.provider.calls != 1 {
		t.Fatalf("reply: %d %s, calls %d", res.status, res.raw, h.provider.calls)
	}
	if !strings.Contains(h.provider.lastDeveloper, "Only an explicit request for a language in <user_instruction>") {
		t.Fatal("the developer message must allow an explicitly requested language")
	}
}

// "ответь на англ" is an explicit request in short form: the English answer
// stays, no repair call rewrites it back into Russian.
func TestAbbreviatedLanguageRequestIsNotRepaired(t *testing.T) {
	h := newHarness(t, withEnv("AI_REPAIR_ENABLED", "true"))
	session := h.signIn("+7 707 610 20 05")
	english := "Hey! I'm good, thanks. How about you?"
	h.provider.replies = []string{english}

	res := h.do(http.MethodPost, "/api/v1/ai/reply", replyBody("Привет! Как дела? Что делаешь сегодня вечером?",
		map[string]any{"instruction": "ответь на англ"}), h.auth(session.access))
	if res.status != http.StatusOK || res.str("reply") != english || h.provider.calls != 1 {
		t.Fatalf("reply: %d %s, calls %d", res.status, res.raw, h.provider.calls)
	}
}

// Kazakh typed in Latin letters is Kazakh; Russian typed in Latin letters is
// left to the model (no language named, nothing verified or repaired).
func TestLatinScriptMessagesOnARussianPhone(t *testing.T) {
	h := newHarness(t, withEnv("AI_REPAIR_ENABLED", "true"))
	session := h.signIn("+7 707 610 20 06")

	res := h.do(http.MethodPost, "/api/v1/ai/reply", replyBody("Salem! Qalaisyn?", nil), h.auth(session.access))
	if res.status != http.StatusOK ||
		!strings.Contains(h.provider.lastDeveloper, "Write the reply in Kazakh, the language of the incoming message.") ||
		!strings.Contains(h.provider.lastDeveloper, "Note: <user_instruction> is written in Russian, but the reply must be in Kazakh.") {
		t.Fatalf("latin kazakh: %d %s", res.status, res.raw)
	}

	before := h.provider.calls
	cyrillic := "Привет! Всё хорошо, спасибо."
	h.provider.replies = []string{cyrillic}
	res = h.do(http.MethodPost, "/api/v1/ai/reply", replyBody("Privet, kak dela?", map[string]any{
		"language": "kk", "input_language": "kk", "instruction": ""}), h.auth(session.access))
	if res.status != http.StatusOK || res.str("reply") != cyrillic || h.provider.calls != before+1 {
		t.Fatalf("transliterated russian: %d %s, calls %d", res.status, res.raw, h.provider.calls-before)
	}
	developer := h.provider.lastDeveloper
	if !strings.Contains(developer, "Write the reply in the language of the incoming message.") ||
		strings.Contains(developer, "Write the reply in English") || strings.Contains(developer, "Write the reply in Uzbek") {
		t.Fatal("transliterated Russian must be mirrored, never forced into English or Uzbek")
	}
}
