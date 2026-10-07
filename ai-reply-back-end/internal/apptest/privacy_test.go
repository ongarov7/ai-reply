package apptest

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const secretMessage = "МЕНІҢ ҚҰПИЯ ХАБАРЛАМАМ ORDER-99817 Астана Сарыарқа 12"
const secretInstruction = "ҚҰПИЯ НҰСҚАУ: жауапта мекенжайды қайталама"

// Ең маңызды тест: хабарлама да, жауап та ешқайда сақталмайды.
func TestMessageContentIsNeverPersisted(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 705 111 22 33")
	h.provider.reply = "ҚҰПИЯ ЖАУАП: тапсырысты тексеріп жатырмын"

	res := h.do(http.MethodPost, "/api/v1/ai/reply", map[string]any{
		"source_text": secretMessage, "instruction": secretInstruction,
		"language": "kk", "template_id": "client",
	}, h.auth(session.access))
	if res.status != http.StatusOK {
		t.Fatalf("generate: %d %s", res.status, res.raw)
	}

	// Промпт шынымен жіберілді (яғни мәтін өңделді).
	if !strings.Contains(h.provider.lastUser, secretMessage) {
		t.Fatal("source text did not reach the provider — the test would prove nothing")
	}
	if !strings.Contains(h.provider.lastUser, secretInstruction) {
		t.Fatal("instruction did not reach the provider")
	}

	// Дерекқор файлында ешқандай ізі болмауы керек.
	for _, needle := range []string{secretMessage, secretInstruction, "ҚҰПИЯ ЖАУАП", "ORDER-99817"} {
		if h.dbContains(needle) {
			t.Fatalf("database contains %q — message content must never be persisted", needle)
		}
	}

	// Журналда да жоқ.
	logs := h.logs.String()
	for _, needle := range []string{secretMessage, secretInstruction, "ҚҰПИЯ ЖАУАП", "1111"} {
		if strings.Contains(logs, needle) {
			t.Fatalf("logs contain %q", needle)
		}
	}

	// Бірақ метадерек жазылған: токен саны, модель, кідіріс.
	events, err := h.store.UserEvents(context.Background(), session.userID, 10)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("usage events = %d, want 1", len(events))
	}
	event := events[0]
	if event.InputTokens != 120 || event.OutputTokens != 40 || event.TotalTokens != 160 {
		t.Fatalf("token accounting wrong: %+v", event)
	}
	if event.Model != "test-model" || event.Status != "success" {
		t.Fatalf("event metadata wrong: %+v", event)
	}
	if event.SourceChars != len([]rune(secretMessage)) {
		t.Fatalf("source_chars = %d, want %d (length only, never the text)", event.SourceChars, len([]rune(secretMessage)))
	}
}

// Әкімші API-інде хабарлама мазмұнын қайтаратын өріс жоқ.
func TestAdminAPINeverExposesContent(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 705 222 33 44")
	h.generate(session.access, secretMessage)

	adminSession := h.signInAdmin()
	detail := h.do(http.MethodGet, "/api/v1/admin/users/"+session.userID, nil,
		adminSession.headers(h.cfg.Admin.CookieName))
	if detail.status != http.StatusOK {
		t.Fatalf("user detail: %d %s", detail.status, detail.raw)
	}
	payload := string(detail.raw)
	for _, forbidden := range []string{secretMessage, "source_text", "reply", "message", "prompt", "conversation"} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("admin payload exposes %q", forbidden)
		}
	}

	// Дашбордта да тек метадерек.
	dashboard := h.do(http.MethodGet, "/api/v1/admin/dashboard?range=30d", nil,
		adminSession.headers(h.cfg.Admin.CookieName))
	if strings.Contains(string(dashboard.raw), secretMessage) {
		t.Fatal("dashboard exposes message content")
	}
}

// Тым ұзын мәтін қабылданбайды (шығын мен промпт шектеуі).
func TestSourceTextLimit(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 705 333 44 55")

	long := strings.Repeat("ә", h.cfg.Limits.SourceTextChars+1)
	res := h.do(http.MethodPost, "/api/v1/ai/reply",
		map[string]any{"source_text": long, "language": "kk"}, h.auth(session.access))
	if res.status != http.StatusBadRequest || res.errorCode() != "INVALID_REQUEST" {
		t.Fatalf("got %d %s, want 400 INVALID_REQUEST", res.status, res.errorCode())
	}
	if h.provider.calls != 0 {
		t.Fatal("oversized request reached the provider")
	}
}

// Токенсіз ешкім AI-ға жетпейді.
func TestAIRequiresAuthentication(t *testing.T) {
	h := newHarness(t)
	res := h.do(http.MethodPost, "/api/v1/ai/reply", map[string]any{"source_text": "сәлем"}, nil)
	if res.status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated request accepted: %d", res.status)
	}
}

// Polish: жазба да, түзетілген нұсқа да ешқайда сақталмайды.
func TestPolishTextIsNeverPersisted(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 705 444 55 66")
	note := "ҚҰПИЯ ЖАЗБА айгерімге айт тапсырыс ORDER-77412 дайын"
	h.provider.reply = "ҚҰПИЯ ЖАЗБА: Айгерімге айт, тапсырыс ORDER-77412 дайын."

	res := h.do(http.MethodPost, "/api/v1/ai/polish", map[string]any{"text": note, "input_language": "kk"},
		h.auth(session.access))
	if res.status != http.StatusOK {
		t.Fatalf("polish: %d %s", res.status, res.raw)
	}
	if !strings.Contains(h.provider.lastUser, note) {
		t.Fatal("the note did not reach the provider — the test would prove nothing")
	}
	for _, needle := range []string{note, h.provider.reply, "ORDER-77412", "ҚҰПИЯ ЖАЗБА"} {
		if h.dbContains(needle) {
			t.Fatalf("database contains %q", needle)
		}
		if strings.Contains(h.logs.String(), needle) {
			t.Fatalf("logs contain %q", needle)
		}
	}
	if !strings.Contains(h.logs.String(), `"mode":"polish"`) {
		t.Fatal("no metadata-only log for polish")
	}
}
