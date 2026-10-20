package apptest

import (
	"net/http"
	"strings"
	"testing"
)

// Алушының жаңа атаулары (colleague, partner) ескі мәндермен бірдей мәнерге
// жетеді. Old builds keep sending work and business forever, newer ones may
// send the new names: the developer prompt must not tell them apart.
func TestRecipientAliasesReachTheSameStyle(t *testing.T) {
	for alias, canonical := range map[string]string{"partner": "business", "colleague": "work"} {
		h := newHarness(t)
		session := h.signIn("+7 707 600 10 21")
		developer := func(id, relationship string) string {
			t.Helper()
			body := map[string]any{"source_text": "Добрый день! Когда пришлёте договор?", "template_id": id, "language": "ru",
				"template": map[string]any{"name": "Партнёр", "relationship": relationship, "tone": "professional",
					"instructions": "", "reply_length": "short", "emoji_policy": "minimal",
					"working_hours_behaviour": "mention_when_relevant"}}
			if res := h.do(http.MethodPost, "/api/v1/ai/reply", body, h.auth(session.access)); res.status != http.StatusOK {
				t.Fatalf("%s reply: %d %s", id, res.status, res.raw)
			}
			return h.provider.lastDeveloper
		}

		want := developer(canonical, canonical)
		if got := developer(alias, alias); got != want {
			t.Errorf("template id %q builds another developer prompt than %q", alias, canonical)
		}
		if got := developer("6f1c2a9e-0b7d-4e8a-9c3f-2d5e8a1b4c70", alias); got != want {
			t.Errorf("relationship %q under a custom id builds another developer prompt than %q", alias, canonical)
		}
		if strings.Contains(want, "Follow the preferences in <template_instructions>") {
			t.Errorf("%s fell back to the custom hint", canonical)
		}
	}
}

// Симулятор да жаңа атауларды қабылдайды.
func TestSimulatorAcceptsRecipientAliases(t *testing.T) {
	h := newHarness(t)
	session := h.signInSimulator()
	headers := session.headers(h.cfg.Admin.CookieName)
	developer := func(id string) string {
		t.Helper()
		res := h.do(http.MethodPost, "/api/v1/simulator/generate", map[string]any{
			"source_text": "Добрый день! Когда пришлёте договор?", "instruction": "", "language": "ru",
			"template_id": id, "platform": "web",
		}, headers)
		if res.status != http.StatusOK {
			t.Fatalf("%s generate: %d %s", id, res.status, res.raw)
		}
		return h.provider.lastDeveloper
	}
	for alias, canonical := range map[string]string{"partner": "business", "colleague": "work"} {
		if developer(alias) != developer(canonical) {
			t.Errorf("simulator %q builds another developer prompt than %q", alias, canonical)
		}
	}
}
