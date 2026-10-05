package apptest

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

const composeInstruction = "Поздравь директора Сакена Бакпакбековича с 55-летием. Очень вежливо и тепло, добавь несколько уместных эмодзи."

func (h *harness) compose(token string, body map[string]any) response {
	if _, ok := body["platform"]; !ok {
		body["platform"] = "ios"
	}
	return h.do(http.MethodPost, "/api/v1/ai/compose", body, h.auth(token))
}

// Compose — жеке режим: көшірілген хабарлама да, жауап ережелері де жоқ.
func TestComposeWritesAStandaloneMessage(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 705 300 30 30")
	h.provider.reply = "Уважаемый Сакен Бакпакбекович! От всей души поздравляем Вас с 55-летием! 🎉"

	res := h.compose(session.access, map[string]any{"instruction": composeInstruction, "language": "ru"})
	if res.status != http.StatusOK {
		t.Fatalf("compose: %d %s", res.status, res.raw)
	}
	if res.str("text") != h.provider.reply {
		t.Fatalf("text = %q", res.str("text"))
	}
	if res.str("detected_language") != "ru" {
		t.Fatalf("detected_language = %q", res.str("detected_language"))
	}
	if res.num("usage", "daily_limit") != 7 || res.num("usage", "remaining_today") != 6 {
		t.Fatalf("usage block: %s", res.raw)
	}

	if !strings.Contains(h.provider.lastUser, composeInstruction) {
		t.Fatal("the instruction did not reach the provider")
	}
	for _, reply := range []string{"<incoming_message>", "<user_instruction>", "as a reply to the incoming message",
		"Match the length of the incoming message"} {
		if strings.Contains(h.provider.lastUser+h.provider.lastDeveloper, reply) {
			t.Fatalf("compose prompt contains the reply-mode part %q", reply)
		}
	}
	if !strings.Contains(h.provider.lastDeveloper, "there is no incoming message") {
		t.Fatal("compose did not use its own developer rules")
	}
	if !strings.Contains(h.provider.lastDeveloper, "Write the message in Russian, the language of the request") {
		t.Fatal("the message language is missing from the developer rules")
	}
	if strings.Contains(h.provider.lastUser, "Write the message in") {
		t.Fatal("the language rule leaked into the user part of the prompt")
	}
	if h.provider.lastMaxTokens < 700 {
		t.Fatalf("compose max_output_tokens = %d, want at least 700", h.provider.lastMaxTokens)
	}
	if strings.Contains(h.provider.lastDeveloper, "ANOTHER VERSION") {
		t.Fatal("a first generation must not ask for another version")
	}
}

// Regenerate — сол нұсқау, жаңа нұсқа; квотадан тағы бір бірлік.
func TestComposeRegenerateAsksForAnotherVersion(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 705 300 30 31")

	first := h.compose(session.access, map[string]any{"instruction": composeInstruction})
	again := h.compose(session.access, map[string]any{"instruction": composeInstruction, "regenerate": true})
	if first.status != http.StatusOK || again.status != http.StatusOK {
		t.Fatalf("compose: %d / %d %s", first.status, again.status, again.raw)
	}
	if !strings.Contains(h.provider.lastDeveloper, "ANOTHER VERSION") {
		t.Fatal("regenerate did not ask for a different version")
	}
	if again.num("usage", "used_today") != 2 {
		t.Fatalf("regenerate must spend one generation: %s", again.raw)
	}
}

// Квота ортақ: жауаптар мен жаңа хабарламалар бір күндік санауышты жұмсайды.
func TestComposeSharesTheDailyQuotaWithReplies(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 705 300 30 32")

	for i := 1; i <= 7; i++ {
		var res response
		if i%2 == 0 {
			res = h.generate(session.access, "Сәлеметсіз бе, бағасы қанша?")
		} else {
			res = h.compose(session.access, map[string]any{"instruction": "Напиши коллеге короткое поздравление с днём рождения"})
		}
		if res.status != http.StatusOK {
			t.Fatalf("generation %d: %d %s", i, res.status, res.raw)
		}
	}

	blocked := h.compose(session.access, map[string]any{"instruction": "Ещё одно сообщение"})
	if blocked.status != http.StatusTooManyRequests || blocked.errorCode() != "DAILY_LIMIT_REACHED" {
		t.Fatalf("8th generation: %d %s, want 429 DAILY_LIMIT_REACHED", blocked.status, blocked.raw)
	}
	if blocked.num("error", "details", "daily_limit") != 7 {
		t.Fatalf("limit details missing: %s", blocked.raw)
	}
	if h.provider.calls != 7 {
		t.Fatalf("provider called %d times, want 7", h.provider.calls)
	}
}

func TestComposeValidatesTheInstruction(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 705 300 30 33")

	empty := h.compose(session.access, map[string]any{"instruction": "   \n "})
	if empty.status != http.StatusBadRequest || empty.errorCode() != "INVALID_REQUEST" {
		t.Fatalf("empty instruction: %d %s", empty.status, empty.raw)
	}
	if empty.str("error", "details", "field") != "instruction" {
		t.Fatalf("empty instruction details: %s", empty.raw)
	}

	// Kazakh letters are multi-byte: the limit counts characters.
	exact := h.compose(session.access, map[string]any{"instruction": strings.Repeat("ә", 400)})
	if exact.status != http.StatusOK {
		t.Fatalf("400 characters rejected: %d %s", exact.status, exact.raw)
	}
	over := h.compose(session.access, map[string]any{"instruction": strings.Repeat("ә", 401)})
	if over.status != http.StatusBadRequest || over.errorCode() != "INVALID_REQUEST" {
		t.Fatalf("401 characters: %d %s", over.status, over.raw)
	}
	if over.str("error", "details", "field") != "instruction" || over.num("error", "details", "max_characters") != 400 {
		t.Fatalf("over-limit details: %s", over.raw)
	}

	// The request body is strict, like /ai/reply: a source_text has no place here.
	unknown := h.compose(session.access, map[string]any{"instruction": "Привет", "source_text": "old clipboard"})
	if unknown.status != http.StatusBadRequest {
		t.Fatalf("unknown field accepted: %d %s", unknown.status, unknown.raw)
	}

	if h.provider.calls != 1 {
		t.Fatalf("invalid requests reached the provider: %d calls", h.provider.calls)
	}
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(session.access))
	if usage.num("used_today") != 1 {
		t.Fatalf("invalid requests spent quota: %s", usage.raw)
	}
}

func TestComposeRequiresASession(t *testing.T) {
	h := newHarness(t)
	res := h.do(http.MethodPost, "/api/v1/ai/compose", map[string]any{"instruction": "Привет"}, nil)
	if res.status != http.StatusUnauthorized {
		t.Fatalf("anonymous compose: %d", res.status)
	}
	expired := h.do(http.MethodPost, "/api/v1/ai/compose", map[string]any{"instruction": "Привет"}, h.auth("not-a-token"))
	if expired.status != http.StatusUnauthorized {
		t.Fatalf("bad token: %d", expired.status)
	}
}

func TestComposeProviderFailureRefundsQuota(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 705 300 30 34")
	h.provider.err = domain.ErrProviderDown

	res := h.compose(session.access, map[string]any{"instruction": composeInstruction})
	if res.status != http.StatusBadGateway || res.errorCode() != "AI_PROVIDER_UNAVAILABLE" {
		t.Fatalf("provider down: %d %s", res.status, res.raw)
	}
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(session.access))
	if usage.num("used_today") != 0 {
		t.Fatalf("a failed compose was charged: %s", usage.raw)
	}

	h.provider.err = domain.ErrProviderTimeout
	timeout := h.compose(session.access, map[string]any{"instruction": composeInstruction})
	if timeout.status != http.StatusGatewayTimeout || timeout.errorCode() != "AI_TIMEOUT" {
		t.Fatalf("provider timeout: %d %s", timeout.status, timeout.raw)
	}
	if !errors.Is(h.provider.err, domain.ErrProviderTimeout) {
		t.Fatal("unexpected provider state")
	}
}

// Нұсқау да, жазылған хабарлама да сақталмайды; оқиғада тек ұзындық пен режим.
func TestComposeContentIsNeverPersisted(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 705 300 30 35")
	secret := "ҚҰПИЯ НҰСҚАУ: Күлжан апайды туған күнімен құттықта, ORDER-55123"
	h.provider.reply = "ҚҰПИЯ ХАБАРЛАМА: Құрметті Күлжан апай!"

	res := h.compose(session.access, map[string]any{"instruction": secret, "language": "kk", "app_version": "1.4.0"})
	if res.status != http.StatusOK {
		t.Fatalf("compose: %d %s", res.status, res.raw)
	}
	for _, needle := range []string{secret, "ORDER-55123", "ҚҰПИЯ ХАБАРЛАМА"} {
		if h.dbContains(needle) {
			t.Fatalf("database contains %q", needle)
		}
		if strings.Contains(h.logs.String(), needle) {
			t.Fatalf("logs contain %q", needle)
		}
	}
	if !strings.Contains(h.logs.String(), `"msg":"ai_reply_generated","user_id":"`+session.userID+`","mode":"compose"`) {
		t.Fatal("no structured success event in the log")
	}

	events, err := h.store.UserEvents(context.Background(), session.userID, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("events: %v %d", err, len(events))
	}
	event := events[0]
	if event.Mode != "compose" || event.Status != "success" || event.Language != "kk" {
		t.Fatalf("event metadata: %+v", event)
	}
	if event.SourceChars != len([]rune(secret)) || event.TotalTokens != 160 || event.AppVersion != "1.4.0" {
		t.Fatalf("event accounting: %+v", event)
	}
}

// Бұрынғы жауап ағыны өзгермеді: оқиға режимі — reply.
func TestReplyEventsKeepTheReplyMode(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 705 300 30 36")
	if res := h.generate(session.access, "Сәлеметсіз бе, бағасы қанша?"); res.status != http.StatusOK {
		t.Fatalf("reply: %d %s", res.status, res.raw)
	}
	if !strings.Contains(h.provider.lastUser, "<incoming_message>") {
		t.Fatal("reply prompt lost its incoming message block")
	}
	events, err := h.store.UserEvents(context.Background(), session.userID, 10)
	if err != nil || len(events) != 1 || events[0].Mode != "reply" {
		t.Fatalf("reply event: %v %+v", err, events)
	}
}

func TestConfigAnnouncesCompose(t *testing.T) {
	h := newHarness(t)
	res := h.do(http.MethodGet, "/api/v1/config", nil, nil)
	features, _ := res.body["features"].(map[string]any)
	if features["compose"] != true {
		t.Fatalf("features = %v, want compose=true", res.body["features"])
	}
}
