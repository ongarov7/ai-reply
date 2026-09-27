package apptest

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// Дүкендегі ескі build-тер өзгеріссіз жұмыс істеуі керек:
// install_id → токен, содан кейін /v1/reply/generate.
func TestLegacyEndpointsStayCompatible(t *testing.T) {
	h := newHarness(t)

	register := h.do(http.MethodPost, "/v1/auth/register",
		map[string]any{"install_id": "5C4D3B2A-1111-2222-3333-444455556666"}, nil)
	if register.status != http.StatusOK {
		t.Fatalf("legacy register: %d %s", register.status, register.raw)
	}
	token := register.str("token")
	if token == "" {
		t.Fatal("legacy register returned no token")
	}
	if expires := register.num("expires_at"); expires <= 0 {
		t.Fatalf("expires_at = %v, want a unix timestamp", expires)
	}

	generate := h.do(http.MethodPost, "/v1/reply/generate", map[string]any{
		"message":     "Сәлеметсіз бе! Жеткізу қанша тұрады?",
		"template_id": "client", "keyboard_language": "kk",
		"user_instruction": "Сыпайы жауап бер",
		"profile": map[string]any{
			"description": "Парфюм сатамын", "role": "сатушы", "preferred_tone": "professional",
			"business": map[string]any{"offering": "Парфюм", "summary": "Астана", "rules": []string{"Бағаны растама"}},
		},
		"template": map[string]any{
			"name": "Клиент", "relationship": "client", "tone": "professional",
			"instructions": "Қысқа жауап", "reply_length": "short", "emoji_policy": "minimal",
			"working_hours_behaviour": "mention_when_relevant",
		},
		"business_context": map[string]any{
			"enabled": true, "is_within_working_hours": false,
			"current_local_time": "22:40", "next_working_period": "ертең 10:00",
		},
	}, h.auth(token))

	if generate.status != http.StatusOK {
		t.Fatalf("legacy generate: %d %s", generate.status, generate.raw)
	}
	if reply := generate.str("reply"); reply == "" {
		t.Fatalf("legacy response missing reply: %s", generate.raw)
	}
	if _, ok := generate.body["detected_language"]; !ok {
		t.Fatalf("legacy response missing detected_language: %s", generate.raw)
	}
	// Android-тың user_instruction өрісі промптқа жетті.
	if !strings.Contains(h.provider.lastUser, "Сыпайы жауап бер") {
		t.Fatal("user_instruction was dropped")
	}
	// Жұмыс уақыты контексі де жетті.
	if !strings.Contains(h.provider.lastUser, "22:40") {
		t.Fatal("working hours context was dropped")
	}
}

// Ескі клиент те квотадан тыс жауап ала алмайды және 429 көреді.
func TestLegacyClientSharesQuota(t *testing.T) {
	h := newHarness(t)
	register := h.do(http.MethodPost, "/v1/auth/register",
		map[string]any{"install_id": "AAAA1111-BBBB-2222-CCCC-333344445555"}, nil)
	token := register.str("token")

	for i := 0; i < 7; i++ {
		res := h.do(http.MethodPost, "/v1/reply/generate",
			map[string]any{"message": "Сұрақ", "keyboard_language": "kk"}, h.auth(token))
		if res.status != http.StatusOK {
			t.Fatalf("legacy generation %d: %d %s", i+1, res.status, res.raw)
		}
	}
	blocked := h.do(http.MethodPost, "/v1/reply/generate",
		map[string]any{"message": "Артық сұрақ", "keyboard_language": "kk"}, h.auth(token))
	if blocked.status != http.StatusTooManyRequests {
		t.Fatalf("legacy limit: %d, want 429", blocked.status)
	}
	if blocked.header.Get("Retry-After") == "" {
		t.Fatal("legacy 429 must carry Retry-After")
	}
	if errObj, ok := blocked.body["error"].(map[string]any); !ok || errObj["code"] != "rate_limited" {
		t.Fatalf("legacy error shape changed: %s", blocked.raw)
	}
}

// Ескі шекте де мәтін ұзындығы тексеріледі (413 + limit/actual).
func TestLegacyMessageTooLong(t *testing.T) {
	h := newHarness(t)
	register := h.do(http.MethodPost, "/v1/auth/register",
		map[string]any{"install_id": "DDDD1111-EEEE-2222-FFFF-333344445555"}, nil)

	long := strings.Repeat("а", h.limits.Current(context.Background()).SourceChars+1)
	res := h.do(http.MethodPost, "/v1/reply/generate",
		map[string]any{"message": long}, h.auth(register.str("token")))
	if res.status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", res.status)
	}
	errObj, _ := res.body["error"].(map[string]any)
	if errObj["code"] != "message_too_long" || errObj["limit"] == nil {
		t.Fatalf("legacy 413 payload changed: %s", res.raw)
	}
}
