package apptest

import (
	"net/http"
	"strings"
	"testing"
)

// A 40-second dictated instruction is longer than a typed one may be: the
// server publishes the room and the apps send instruction_source "voice".
func TestVoiceInstructionHasTheRoomOfAFortySecondTurn(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 705 410 41 41")

	config := h.do(http.MethodGet, "/api/v1/config", nil, nil)
	if got := config.num("max_voice_instruction_length"); got != 1000 {
		t.Fatalf("max_voice_instruction_length = %v (%s)", got, config.raw)
	}
	if got := config.num("max_instruction_length"); got != 400 {
		t.Fatalf("typed limit changed: max_instruction_length = %v", got)
	}
	features, _ := config.body["features"].(map[string]any)
	if features["voice_instruction"] != true {
		t.Fatalf("features.voice_instruction missing: %v", config.body["features"])
	}

	// Kazakh letters are multi-byte: the limit counts characters.
	dictated := strings.Repeat("ә", 1000)
	voice := h.compose(session.access, map[string]any{"instruction": dictated, "instruction_source": "voice"})
	if voice.status != http.StatusOK {
		t.Fatalf("1000 dictated characters rejected: %d %s", voice.status, voice.raw)
	}
	if !strings.Contains(h.provider.lastUser, dictated) {
		t.Fatal("the dictated instruction did not reach the provider whole")
	}

	over := h.compose(session.access, map[string]any{"instruction": strings.Repeat("ә", 1001), "instruction_source": "voice"})
	if over.status != http.StatusBadRequest || over.num("error", "details", "max_characters") != 1000 {
		t.Fatalf("1001 dictated characters: %d %s", over.status, over.raw)
	}

	typed := h.compose(session.access, map[string]any{"instruction": strings.Repeat("ә", 401)})
	if typed.status != http.StatusBadRequest || typed.num("error", "details", "max_characters") != 400 {
		t.Fatalf("typed 401 characters: %d %s", typed.status, typed.raw)
	}
	other := h.compose(session.access, map[string]any{"instruction": strings.Repeat("ә", 401), "instruction_source": "keyboard"})
	if other.status != http.StatusBadRequest || other.num("error", "details", "max_characters") != 400 {
		t.Fatalf("an unknown source widened the typed limit: %d %s", other.status, other.raw)
	}
	if h.provider.calls != 1 {
		t.Fatalf("rejected requests reached the provider: %d calls", h.provider.calls)
	}
}

// A reply's instruction is clamped, never refused. A dictated one keeps the
// end of its 40 seconds; a typed one is still cut at the typed limit.
func TestVoiceReplyInstructionKeepsItsEnd(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 705 420 42 42")
	ending := "СОҢЫНДА РАХМЕТ АЙТ"
	dictated := strings.Repeat("ә", 700) + " " + ending
	reply := func(source string) response {
		body := map[string]any{
			"source_text": "Ертең кездесеміз бе?", "instruction": dictated, "language": "kk",
			"template_id": "client", "platform": "ios", "app_version": "1.3.0",
		}
		if source != "" {
			body["instruction_source"] = source
		}
		return h.do(http.MethodPost, "/api/v1/ai/reply", body, h.auth(session.access))
	}

	if res := reply("voice"); res.status != http.StatusOK {
		t.Fatalf("voice reply: %d %s", res.status, res.raw)
	}
	if !strings.Contains(h.provider.lastUser, ending) {
		t.Fatal("the end of the dictated instruction was cut")
	}
	if res := reply(""); res.status != http.StatusOK {
		t.Fatalf("typed reply: %d %s", res.status, res.raw)
	}
	if strings.Contains(h.provider.lastUser, ending) {
		t.Fatal("a typed instruction is no longer clamped at the typed limit")
	}
}
