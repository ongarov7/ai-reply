package apptest

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const polishNote = "скажи ей что сегодня не получится давай завтра вечером"

func (h *harness) polish(token string, body map[string]any) response {
	return h.do(http.MethodPost, "/api/v1/ai/polish", body, h.auth(token))
}

func TestPolishSuggestsACleanerNote(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 708 400 20 01")
	h.provider.reply = "Скажи ей, что сегодня не получится, давай завтра вечером."

	res := h.polish(session.access, map[string]any{"text": "  " + polishNote + " ", "input_language": "ru",
		"platform": "ios", "app_version": "1.5.0"})
	if res.status != http.StatusOK || res.str("text") != h.provider.reply || res.body["changed"] != true {
		t.Fatalf("polish: %d %s", res.status, res.raw)
	}
	if h.provider.lastUser != "<note>\n"+polishNote+"\n</note>" || strings.Contains(h.provider.lastDeveloper, polishNote) {
		t.Fatalf("the note must stay in the user message: %q", h.provider.lastUser)
	}
	if !strings.Contains(h.provider.lastDeveloper, "Do not answer the note") || h.provider.lastMaxTokens != 200 {
		t.Fatalf("polish prompt: tokens %d", h.provider.lastMaxTokens)
	}

	// Квота жұмсалмайды, бірақ токен мен оқиға есептеледі.
	if usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(session.access)); usage.num("used_today") != 0 {
		t.Fatalf("polish spent quota: %s", usage.raw)
	}
	var used, inputTokens int
	if err := h.db.Reader().QueryRow(`SELECT used, input_tokens FROM usage_daily WHERE user_id = ?`,
		session.userID).Scan(&used, &inputTokens); err != nil || used != 0 || inputTokens != 120 {
		t.Fatalf("usage_daily: used %d, input tokens %d (%v)", used, inputTokens, err)
	}
	events, err := h.store.UserEvents(context.Background(), session.userID, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("events: %v %d", err, len(events))
	}
	if e := events[0]; e.Mode != "polish" || e.PromptVersion != "polish_v1" || e.Status != "success" ||
		e.SourceChars != len([]rune(polishNote)) || e.Language != "ru" || e.AppVersion != "1.5.0" {
		t.Fatalf("event: %+v", e)
	}
}

// Модель фактіні өзгертсе не ештеңе өзгертпесе — ұсыныс жоқ.
func TestPolishGuardReturnsNoSuggestion(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 708 400 20 02")
	cases := []struct{ name, note, output string }{
		{"number", "скажи что буду в 18:00", "Скажи, что буду в 19:00."},
		{"name", "передай Ерлану что приду", "Передай Ерлану, что приду к Айгерим."},
		{"url", "скинь ссылку https://ai-reply.kz/offer", "Скинь ссылку https://ai-reply.kz."},
		{"unchanged", "Ответь вежливо, что приду.", "Ответь вежливо, что приду."},
	}
	for _, c := range cases {
		h.provider.reply = c.output
		res := h.polish(session.access, map[string]any{"text": c.note})
		if res.status != http.StatusOK || res.body["changed"] != false || res.str("text") != c.note {
			t.Fatalf("%s: %d %s", c.name, res.status, res.raw)
		}
	}
}

func TestPolishValidatesTheText(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 708 400 20 03")

	empty := h.polish(session.access, map[string]any{"text": "  \n "})
	if empty.status != http.StatusBadRequest || empty.errorCode() != "INVALID_REQUEST" || empty.str("error", "details", "field") != "text" {
		t.Fatalf("empty: %d %s", empty.status, empty.raw)
	}
	long := h.polish(session.access, map[string]any{"text": strings.Repeat("ә", 401)})
	if long.status != http.StatusBadRequest || long.str("error", "details", "field") != "text" ||
		long.num("error", "details", "max_characters") != 400 {
		t.Fatalf("too long: %d %s", long.status, long.raw)
	}
	if anonymous := h.do(http.MethodPost, "/api/v1/ai/polish", map[string]any{"text": polishNote}, nil); anonymous.status != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", anonymous.status)
	}
	if h.provider.calls != 0 {
		t.Fatalf("invalid requests reached the provider: %d", h.provider.calls)
	}
}

// Polish пен генерацияның лимиттері бір-біріне әсер етпейді.
func TestPolishHasItsOwnRateLimit(t *testing.T) {
	h := newHarness(t, withEnv("RATE_POLISH_PER_MINUTE", "2"), withEnv("RATE_AI_PER_MINUTE", "1"))
	session := h.signIn("+7 708 400 20 04")

	for i := 0; i < 2; i++ {
		if res := h.polish(session.access, map[string]any{"text": polishNote}); res.status != http.StatusOK {
			t.Fatalf("polish %d: %d %s", i+1, res.status, res.raw)
		}
	}
	if limited := h.polish(session.access, map[string]any{"text": polishNote}); limited.status != http.StatusTooManyRequests ||
		limited.errorCode() != "RATE_LIMITED" {
		t.Fatalf("third polish: %d %s", limited.status, limited.raw)
	}
	if res := h.generate(session.access, "Сәлеметсіз бе, бағасы қанша?"); res.status != http.StatusOK {
		t.Fatalf("polish used up the reply bucket: %d %s", res.status, res.raw)
	}
	if res := h.generate(session.access, "Тағы бір сұрақ"); res.status != http.StatusTooManyRequests {
		t.Fatalf("the reply bucket is its own: %d", res.status)
	}
}

func TestPolishCanBeSwitchedOff(t *testing.T) {
	h := newHarness(t, withEnv("AI_POLISH_ENABLED", "false"))
	session := h.signIn("+7 708 400 20 05")

	features, _ := h.do(http.MethodGet, "/api/v1/config", nil, nil).body["features"].(map[string]any)
	if features["instruction_polish"] != false {
		t.Fatalf("features = %v", features)
	}
	res := h.polish(session.access, map[string]any{"text": polishNote})
	if res.status != http.StatusNotFound || res.errorCode() != "NOT_FOUND" || h.provider.calls != 0 {
		t.Fatalf("disabled polish: %d %s, calls %d", res.status, res.raw, h.provider.calls)
	}
}

// Күндік лимит біткенде де polish жұмыс істейді, ал polish квотаны жемейді.
func TestPolishDoesNotSpendTheDailyQuota(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 708 400 20 06")
	for i := 0; i < 3; i++ {
		h.polish(session.access, map[string]any{"text": polishNote})
	}
	for i := 1; i <= 7; i++ {
		if res := h.generate(session.access, "Сәлеметсіз бе, бағасы қанша?"); res.status != http.StatusOK {
			t.Fatalf("generation %d after polishing: %d %s", i, res.status, res.raw)
		}
	}
	if blocked := h.generate(session.access, "Тағы бір сұрақ"); blocked.errorCode() != "DAILY_LIMIT_REACHED" {
		t.Fatalf("8th generation: %s", blocked.raw)
	}
	if res := h.polish(session.access, map[string]any{"text": polishNote}); res.status != http.StatusOK {
		t.Fatalf("polish after the daily limit: %d %s", res.status, res.raw)
	}
}

// Әкімші графигіндегі «генерациялар» — жауап пен жаңа хабарлама; polish
// тек токен мен құнға кіреді.
func TestDashboardGenerationsExcludePolish(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 708 400 20 07")
	h.generate(session.access, "Сәлеметсіз бе, бағасы қанша?")
	h.polish(session.access, map[string]any{"text": polishNote})
	h.polish(session.access, map[string]any{"text": polishNote})

	admin := h.signInAdmin()
	dashboard := h.do(http.MethodGet, "/api/v1/admin/dashboard?range=30d", nil, admin.headers(h.cfg.Admin.CookieName))
	if dashboard.status != http.StatusOK {
		t.Fatalf("dashboard: %d", dashboard.status)
	}
	if dashboard.num("stats", "requests_range") != 1 || dashboard.num("stats", "succeeded") != 1 {
		t.Fatalf("polish counted as a generation: %s", dashboard.raw)
	}
	if dashboard.num("stats", "total_tokens") != 480 {
		t.Fatalf("polish tokens must still be counted: %v", dashboard.num("stats", "total_tokens"))
	}
	series, _ := dashboard.body["series"].(map[string]any)
	generations, _ := series["generations"].([]any)
	total := 0.0
	for _, point := range generations {
		total += point.(map[string]any)["value"].(float64)
	}
	if total != 1 {
		t.Fatalf("generations series = %v", generations)
	}
}
