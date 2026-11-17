package apptest

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// The account AI context (docs/web-cabinet-ai-context.md): saved once in the
// website cabinet, added by the server to every reply and compose, priced at
// 1 reply + one more per 400 characters of context, at most +2, Pro free.

// saveAIContext — PUT /api/v1/me/ai-context with a full document; doc overrides
// the empty defaults.
func (h *harness) saveAIContext(access string, doc map[string]any, revision int) response {
	full := map[string]any{"enabled": true, "roles": []string{}, "occupation": "", "traits": []string{},
		"about": "", "audience": "", "recipients": map[string]any{}}
	for key, value := range doc {
		full[key] = value
	}
	return h.do(http.MethodPut, "/api/v1/me/ai-context", map[string]any{"context": full, "expected_revision": revision}, h.auth(access))
}

func (h *harness) replyTo(access string, body map[string]any, headers ...map[string]string) response {
	all := map[string]any{"source_text": "Сәлеметсіз бе! Гүл бар ма?", "language": "ru", "platform": "ios", "app_version": "1.0.0"}
	for key, value := range body {
		all[key] = value
	}
	auth := h.auth(access)
	for _, extra := range headers {
		for key, value := range extra {
			auth[key] = value
		}
	}
	return h.do(http.MethodPost, "/api/v1/ai/reply", all, auth)
}

func (h *harness) lastPrompt() (string, string) {
	h.provider.mu.Lock()
	defer h.provider.mu.Unlock()
	if len(h.provider.prompts) == 0 {
		h.t.Fatal("no prompt reached the provider")
	}
	p := h.provider.prompts[len(h.provider.prompts)-1]
	return p.User, p.Version
}

func mapAt(r response, path ...string) map[string]any {
	current := any(r.body)
	for _, key := range path {
		m, _ := current.(map[string]any)
		current = m[key]
	}
	m, _ := current.(map[string]any)
	return m
}

// florist — a context whose general part is 222 characters (a reply costs 2)
// and whose client notes add 313 more (535 → 3, the cap of +2).
func florist() map[string]any {
	return map[string]any{
		"roles":      []string{"seller", "business_owner", "seller"},
		"occupation": "Флорист",
		"traits":     []string{"brief", "no_emoji"},
		"about":      strings.Repeat("Цветочный магазин в Алматы. ", 7) + "Доставка за 2 часа.",
		"recipients": map[string]any{
			"client":    map[string]any{"notes": strings.Repeat("На «Вы», вежливо, предлагаю открытку. ", 8) + "Спасибо!!"},
			"colleague": map[string]any{"notes": "Коротко и по делу"},
		},
	}
}

func TestAIContextRoundTripValidationAndConflicts(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("ai-context-round@example.com")

	empty := h.do(http.MethodGet, "/api/v1/me/ai-context", nil, h.auth(s.access))
	mustStatus(t, empty, http.StatusOK, "")
	if empty.num("revision") != 0 || empty.str("updated_at") != "" || mapAt(empty, "context")["enabled"] != true ||
		len(mapAt(empty, "context", "recipients")) != 4 || empty.num("cost", "general") != 1 || empty.num("cost", "client") != 1 {
		t.Fatalf("default document: %s", empty.raw)
	}
	if empty.num("pricing", "chars_per_reply") != 400 || empty.num("pricing", "free_chars") != 0 ||
		empty.num("pricing", "max_extra_replies") != 2 || mapAt(empty, "pricing")["unlimited"] != false {
		t.Fatalf("pricing: %s", empty.raw)
	}
	if roles, _ := mapAt(empty, "catalog")["roles"].([]any); len(roles) != 19 {
		t.Fatalf("role catalog: %s", empty.raw)
	}

	saved := h.saveAIContext(s.access, florist(), 0)
	mustStatus(t, saved, http.StatusOK, "")
	if saved.num("revision") != 1 || saved.str("updated_at") == "" {
		t.Fatalf("first save: %s", saved.raw)
	}
	if roles, _ := mapAt(saved, "context")["roles"].([]any); len(roles) != 2 || roles[0] != "seller" || roles[1] != "business_owner" {
		t.Fatalf("roles are de-duplicated in order: %v", roles)
	}
	if saved.num("cost", "base_chars") != 222 || saved.num("cost", "general") != 2 || saved.num("cost", "friend") != 2 ||
		saved.num("cost", "client") != 3 || saved.num("cost", "colleague") != 2 || saved.num("cost", "partner") != 2 {
		t.Fatalf("cost table: %s", saved.raw)
	}

	stale := h.saveAIContext(s.access, map[string]any{"about": "Другое"}, 0)
	mustStatus(t, stale, http.StatusConflict, "AI_CONTEXT_CHANGED")
	if mapAt(stale, "error", "details")["revision"] != float64(1) {
		t.Fatalf("conflict details: %s", stale.raw)
	}

	// POST is the same as PUT (Android's HttpURLConnection).
	alias := h.do(http.MethodPost, "/api/v1/me/ai-context", map[string]any{"context": map[string]any{
		"enabled": true, "about": "Строка 1\r\nСтрока 2  "}, "expected_revision": 1}, h.auth(s.access))
	mustStatus(t, alias, http.StatusOK, "")
	if alias.num("revision") != 2 || mapAt(alias, "context")["about"] != "Строка 1\nСтрока 2" {
		t.Fatalf("POST alias / normalisation: %s", alias.raw)
	}

	for name, tc := range map[string]struct {
		body             map[string]any
		field, reason    string
		max              float64
		withoutRevision  bool
		withoutEnabledOK bool
	}{
		"about too long":     {body: map[string]any{"about": strings.Repeat("я", 2001)}, field: "about", reason: "too_long", max: 2000},
		"notes too long":     {body: map[string]any{"recipients": map[string]any{"friend": map[string]any{"notes": strings.Repeat("😀", 1001)}}}, field: "recipients.friend.notes", reason: "too_long", max: 1000},
		"unknown role":       {body: map[string]any{"roles": []string{"astronaut"}}, field: "roles", reason: "unknown"},
		"too many traits":    {body: map[string]any{"traits": []string{"introvert", "extrovert", "brief", "detailed", "formal", "casual", "humor"}}, field: "traits", reason: "too_many", max: 6},
		"unknown recipient":  {body: map[string]any{"recipients": map[string]any{"boss": map[string]any{"notes": "x"}}}, field: "recipients", reason: "unknown"},
		"control character":  {body: map[string]any{"occupation": "Флорист\u0007"}, field: "occupation", reason: "invalid_text"},
		"line separator":     {body: map[string]any{"audience": "a\u2028b"}, field: "audience", reason: "invalid_text"},
		"missing revision":   {body: map[string]any{}, field: "expected_revision", reason: "required", withoutRevision: true},
		"emoji are one char": {body: map[string]any{"about": strings.Repeat("😀", 2000)}, withoutEnabledOK: true},
	} {
		t.Run(name, func(t *testing.T) {
			payload := map[string]any{"context": func() map[string]any {
				doc := map[string]any{"enabled": true}
				for k, v := range tc.body {
					doc[k] = v
				}
				return doc
			}()}
			if !tc.withoutRevision {
				payload["expected_revision"] = 2
			}
			res := h.do(http.MethodPut, "/api/v1/me/ai-context", payload, h.auth(s.access))
			if tc.withoutEnabledOK {
				mustStatus(t, res, http.StatusOK, "")
				// Put the stored revision back for the other cases.
				h.exec(`UPDATE ai_contexts SET revision = 2 WHERE user_id = ?`, s.userID)
				return
			}
			mustStatus(t, res, http.StatusBadRequest, "INVALID_REQUEST")
			details := mapAt(res, "error", "details")
			if details["field"] != tc.field || details["reason"] != tc.reason || (tc.max > 0 && details["max"] != tc.max) {
				t.Fatalf("details %v, want %s %s %v", details, tc.field, tc.reason, tc.max)
			}
		})
	}
	noEnabled := h.do(http.MethodPut, "/api/v1/me/ai-context", map[string]any{"context": map[string]any{"about": "x"}, "expected_revision": 2}, h.auth(s.access))
	mustStatus(t, noEnabled, http.StatusBadRequest, "INVALID_REQUEST")
	unknownField := h.do(http.MethodPut, "/api/v1/me/ai-context", map[string]any{"context": map[string]any{"enabled": true, "secret": "x"}, "expected_revision": 2}, h.auth(s.access))
	mustStatus(t, unknownField, http.StatusBadRequest, "INVALID_REQUEST")

	// The document never reaches the log.
	if strings.Contains(h.logs.String(), "Цветочный магазин") || strings.Contains(h.logs.String(), "открытку") {
		t.Fatal("the AI context text was logged")
	}
	config := h.do(http.MethodGet, "/api/v1/config", nil, nil)
	if mapAt(config, "features")["ai_context"] != true || config.num("ai_context_pricing", "chars_per_reply") != 400 {
		t.Fatalf("config: %s", config.raw)
	}
	if h.do(http.MethodGet, "/api/v1/me/ai-context", nil, nil).status != http.StatusUnauthorized {
		t.Fatal("the document needs a signed-in account")
	}
}

func TestAIContextIsAddedToEveryReplyAndChargedPerCharacters(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("ai-context-charge@example.com")
	mustStatus(t, h.saveAIContext(s.access, florist(), 0), http.StatusOK, "")

	client := h.replyTo(s.access, map[string]any{"template_id": "client"})
	mustStatus(t, client, http.StatusOK, "")
	if client.num("units") != 3 || client.body["context_applied"] != true || client.num("usage", "used_today") != 3 {
		t.Fatalf("client reply: %s", client.raw)
	}
	user, version := h.lastPrompt()
	for _, want := range []string{"<account_context>", "Roles: seller; business owner", "Occupation: Флорист",
		"Communication style: writes briefly; avoids emoji", "Цветочный магазин в Алматы", "<recipient_notes>",
		"recipient (client)", "предлагаю открытку"} {
		if !strings.Contains(user, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, user)
		}
	}
	if !strings.HasSuffix(version, "+account_context_v1") {
		t.Fatalf("prompt version %q", version)
	}
	if strings.Index(user, "<account_context>") > strings.Index(user, "<incoming_message>") {
		t.Fatal("the account context must come before the incoming message")
	}

	friend := h.replyTo(s.access, map[string]any{"template_id": "friend"})
	if friend.num("units") != 2 || friend.num("usage", "used_today") != 5 {
		t.Fatalf("friend reply: %s", friend.raw)
	}
	if user, _ := h.lastPrompt(); strings.Contains(user, "<recipient_notes>") || !strings.Contains(user, "<account_context>") {
		t.Fatalf("friend has no notes, only the general context:\n%s", user)
	}

	// work = colleague (the apps' id); a custom persona gets the general part only.
	h.useQuota(s.userID, 0)
	colleague := h.replyTo(s.access, map[string]any{"template_id": "work"})
	if user, _ := h.lastPrompt(); colleague.num("units") != 2 || !strings.Contains(user, "recipient (colleague)") {
		t.Fatalf("work → colleague: %s\n%s", colleague.raw, user)
	}
	custom := h.replyTo(s.access, map[string]any{"template_id": "9d1c5e1a-0000-4000-8000-000000000001",
		"template": map[string]any{"name": "Мама", "relationship": "custom", "tone": "friendly", "instructions": "",
			"reply_length": "short", "emoji_policy": "allowed", "working_hours_behaviour": "ignore"}})
	if user, _ := h.lastPrompt(); custom.num("units") != 2 || strings.Contains(user, "<recipient_notes>") {
		t.Fatalf("custom persona: %s\n%s", custom.raw, user)
	}

	// Compose gets the same context and price (no recipient on mobile).
	composed := h.compose(s.access, map[string]any{"instruction": "Поздравь клиентку с днём рождения", "language": "ru", "platform": "ios"})
	if user, version := h.lastPrompt(); composed.status != http.StatusOK || composed.num("units") != 2 ||
		!strings.Contains(user, "<account_context>") || !strings.Contains(version, "compose") || !strings.HasSuffix(version, "+account_context_v1") {
		t.Fatalf("compose: %s\n%s", composed.raw, user)
	}

	// The usage events record units and context characters, never the text.
	if n := h.scalar(`SELECT units FROM ai_usage_events WHERE user_id = ? AND mode = 'compose'`, s.userID); n != 2 {
		t.Fatalf("compose event units %d", n)
	}
	if n := h.scalar(`SELECT MAX(context_chars) FROM ai_usage_events WHERE user_id = ?`, s.userID); n != 535 {
		t.Fatalf("context chars %d", n)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM usage_reservations WHERE user_id = ? AND mode = 'context' AND status = 'committed'`, s.userID); n != 2+1+1+1+1 {
		t.Fatalf("committed surcharge reservations %d", n)
	}
}

func TestAIContextSurchargeFallsBackToThePlainReplyWhenTheBalanceIsShort(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("ai-context-short@example.com")
	mustStatus(t, h.saveAIContext(s.access, florist(), 0), http.StatusOK, "")
	h.useQuota(s.userID, 5) // 2 of the free plan's 7 replies left today

	res := h.replyTo(s.access, map[string]any{"template_id": "client"})
	mustStatus(t, res, http.StatusOK, "")
	if res.num("units") != 1 || res.body["context_applied"] != false || res.num("usage", "used_today") != 6 {
		t.Fatalf("short balance: %s", res.raw)
	}
	if user, version := h.lastPrompt(); strings.Contains(user, "<account_context>") || strings.Contains(version, "account_context") {
		t.Fatalf("fallback still carries the context:\n%s", user)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM usage_reservations WHERE user_id = ? AND mode = 'context' AND status = 'committed'`, s.userID); n != 0 {
		t.Fatalf("a refunded surcharge stayed charged: %d", n)
	}
	last := h.replyTo(s.access, map[string]any{"template_id": "client"})
	if last.num("units") != 1 || last.num("usage", "used_today") != 7 {
		t.Fatalf("last reply of the day: %s", last.raw)
	}
	mustStatus(t, h.replyTo(s.access, map[string]any{"template_id": "client"}), http.StatusTooManyRequests, "DAILY_LIMIT_REACHED")
	if used := h.entitlement(s.userID).UsedToday; used != 7 {
		t.Fatalf("used today %d", used)
	}
}

func TestAIContextSurchargeIsPaidFromPacksAndRefundedWhenTheProviderFails(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("ai-context-pack@example.com")
	mustStatus(t, h.saveAIContext(s.access, florist(), 0), http.StatusOK, "")
	now := h.clock.Now()
	h.addGrant(s.userID, domain.GrantPack, 10, now, nil, now)
	h.useQuota(s.userID, 7) // the free plan is used up: everything comes from the pack

	h.provider.mu.Lock()
	h.provider.err = domain.ErrProviderDown
	h.provider.mu.Unlock()
	failed := h.replyTo(s.access, map[string]any{"template_id": "client"})
	if failed.status < 500 {
		t.Fatalf("provider failure: %s", failed.raw)
	}
	if left := h.creditsLeft(s.access); left != 10 {
		t.Fatalf("a failed reply kept %v pack replies, want all 10 back", 10-left)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM usage_reservations WHERE user_id = ? AND status = 'reserved'`, s.userID); n != 0 {
		t.Fatalf("%d reservations left open", n)
	}

	h.provider.mu.Lock()
	h.provider.err = nil
	h.provider.mu.Unlock()
	paid := h.replyTo(s.access, map[string]any{"template_id": "client"})
	if paid.num("units") != 3 || paid.body["context_applied"] != true || paid.num("usage", "credits") != 7 {
		t.Fatalf("paid from the pack: %s", paid.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id = ? AND entry = 'consume'`, s.userID); n < 3 {
		t.Fatalf("ledger consume entries %d", n)
	}
}

func TestAIContextReplayIsFreeAndDisabledContextCostsNothing(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("ai-context-replay@example.com")
	mustStatus(t, h.saveAIContext(s.access, florist(), 0), http.StatusOK, "")

	key := map[string]string{"Idempotency-Key": "ctx-replay-0001"}
	first := h.replyTo(s.access, map[string]any{"template_id": "client"}, key)
	again := h.replyTo(s.access, map[string]any{"template_id": "client"}, key)
	if first.num("units") != 3 || again.status != http.StatusOK || again.num("units") != 0 || again.num("usage", "used_today") != 3 {
		t.Fatalf("replay: first %s again %s", first.raw, again.raw)
	}
	if user, _ := h.lastPrompt(); !strings.Contains(user, "<account_context>") {
		t.Fatal("a free regeneration keeps the context")
	}

	off := florist()
	off["enabled"] = false
	saved := h.saveAIContext(s.access, off, 1)
	if saved.num("cost", "client") != 1 || saved.num("cost", "general") != 1 {
		t.Fatalf("switched off costs 1: %s", saved.raw)
	}
	plain := h.replyTo(s.access, map[string]any{"template_id": "client"})
	if user, _ := h.lastPrompt(); plain.num("units") != 1 || plain.body["context_applied"] != false || strings.Contains(user, "<account_context>") {
		t.Fatalf("switched off: %s\n%s", plain.raw, user)
	}
}

func TestAIContextProPaysNoSurcharge(t *testing.T) {
	h := newHarness(t, withEnv("TEST_APPROVED_ENTITLEMENTS", "true"))
	s := h.signIn("ai-context-pro@example.com")
	now := h.clock.Now()
	h.bankPro("ctx-pro-1", s.userID, now.Add(-time.Hour), now.Add(30*24*time.Hour))
	saved := h.saveAIContext(s.access, florist(), 0)
	if mapAt(saved, "pricing")["unlimited"] != true || saved.num("cost", "client") != 1 {
		t.Fatalf("Pro cost table: %s", saved.raw)
	}
	res := h.replyTo(s.access, map[string]any{"template_id": "client"})
	if res.num("units") != 1 || res.body["context_applied"] != true || res.body["usage"].(map[string]any)["unlimited"] != true {
		t.Fatalf("Pro reply: %s", res.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM usage_reservations WHERE user_id = ? AND mode = 'context'`, s.userID); n != 0 {
		t.Fatalf("Pro paid %d surcharge reservations", n)
	}
}

func TestAIContextPricingIsAnAdminSetting(t *testing.T) {
	h := newHarness(t)
	admin := h.signInAdmin()
	settings := h.adminCall(admin, http.MethodGet, "/settings", nil)
	if settings.num("ai_context", "current", "chars_per_reply") != 400 || settings.num("ai_context", "defaults", "max_extra_replies") != 2 {
		t.Fatalf("admin settings: %s", settings.raw)
	}
	bad := h.adminCall(admin, http.MethodPost, "/settings/ai-context", map[string]any{"chars_per_reply": 400, "free_chars": 0, "max_extra_replies": 11})
	mustStatus(t, bad, http.StatusBadRequest, "INVALID_REQUEST")
	saved := h.adminCall(admin, http.MethodPost, "/settings/ai-context", map[string]any{"chars_per_reply": 1000, "free_chars": 300, "max_extra_replies": 1})
	mustStatus(t, saved, http.StatusOK, "")
	if saved.num("current", "free_chars") != 300 {
		t.Fatalf("saved pricing: %s", saved.raw)
	}

	s := h.signIn("ai-context-admin@example.com")
	doc := h.saveAIContext(s.access, florist(), 0)
	// general 222 ≤ 300 free → 1; client 535 → ceil(235/1000) = 1 extra → 2.
	if doc.num("cost", "general") != 1 || doc.num("cost", "client") != 2 {
		t.Fatalf("cost after the admin change: %s", doc.raw)
	}
	if res := h.replyTo(s.access, map[string]any{"template_id": "client"}); res.num("units") != 2 {
		t.Fatalf("reply after the admin change: %s", res.raw)
	}
	off := h.adminCall(admin, http.MethodPost, "/settings/ai-context", map[string]any{"chars_per_reply": 0, "free_chars": 0, "max_extra_replies": 2})
	mustStatus(t, off, http.StatusOK, "")
	if res := h.replyTo(s.access, map[string]any{"template_id": "client"}); res.num("units") != 1 || res.body["context_applied"] != true {
		t.Fatalf("surcharge switched off: %s", res.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action = 'settings.ai_context_pricing.update'`); n != 2 {
		t.Fatalf("audit rows %d", n)
	}
}

func TestAIContextDesktopBudgetCountsOnlyTheDesktopText(t *testing.T) {
	h := newHarness(t)
	s, _ := h.signInProDevice("ai-context-mac@example.com", "ai-context-mac-0001", "macos", "")
	big := florist()
	big["about"] = strings.Repeat("Большой рассказ о себе и своих делах. ", 52) // ≈2 000 characters, ≈3 800 bytes
	mustStatus(t, h.saveAIContext(s.access, big, 0), http.StatusOK, "")
	// A desktop reference of 1 700 bytes: with the ≈4 400-byte account context
	// the prompt is far over the 4 096-byte desktop budget, which counts the
	// desktop's own text only.
	reference := strings.Repeat("Цена букета 15000 тг. ", 50)
	res := h.compose(s.access, map[string]any{"instruction": "Напиши клиенту о цене", "language": "ru", "platform": "macos",
		"context": map[string]any{"references": []string{reference}}, "output_char_limit": 400, "template_id": "client"})
	mustStatus(t, res, http.StatusOK, "")
	if user, _ := h.lastPrompt(); !strings.Contains(user, "<account_context>") || !strings.Contains(user, "<context_reference>") ||
		!strings.Contains(user, "<recipient_notes>") || res.num("units") != 3 {
		t.Fatalf("desktop compose: %s\n%s", res.raw, user)
	}
}
