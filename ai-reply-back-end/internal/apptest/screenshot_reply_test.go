package apptest

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/legal"
)

// Screenshot replies (docs/SCREENSHOT_REPLIES.md): the price is shown before,
// the most it can cost is approved and reserved, the real tokens are charged
// afterwards, the rest refunded. The test model's price row makes one output
// token count as 4 input tokens: one credit = 1 500 + 4 × 180 = 2 220.

const screenshotSecret = "We really need your help with the project."

func projectConversation() map[string]any {
	return map[string]any{"app": "whatsapp", "title": "Aigerim", "messages": []any{
		map[string]any{"id": "1", "role": "other", "sender": "Aigerim", "text": "Hi! Are you available tomorrow?", "time": "12:30"},
		map[string]any{"id": "2", "role": "self", "text": "I'm not sure yet."},
		map[string]any{"id": "3", "role": "other", "text": screenshotSecret},
	}}
}

// longConversation — enough recognised text that the most it may cost is two
// replies (the server's own count is about 2 800 input tokens).
func longConversation() map[string]any {
	var messages []any
	for i := range 20 {
		role := "other"
		if i%2 == 1 {
			role = "self"
		}
		messages = append(messages, map[string]any{"id": "m" + strconv.Itoa(i), "role": role,
			"text": strings.Repeat("Мы обсуждаем сроки проекта и кто что делает на этой неделе. ", 4)})
	}
	return map[string]any{"messages": messages}
}

func screenshotBody(extra map[string]any) map[string]any {
	body := map[string]any{"mode": "text", "conversation": projectConversation(), "language": "ru",
		"platform": "ios", "app_version": "1.4.0"}
	for key, value := range extra {
		body[key] = value
	}
	return body
}

func (h *harness) screenshotEstimate(access string, body map[string]any) response {
	return h.do(http.MethodPost, "/api/v1/ai/screenshot/estimate", body, h.auth(access))
}

func (h *harness) screenshotReply(access string, body map[string]any, key string) response {
	headers := h.auth(access)
	if key != "" {
		headers["Idempotency-Key"] = key
	}
	return h.do(http.MethodPost, "/api/v1/ai/screenshot/reply", body, headers)
}

func (h *harness) usedToday(userID string) int {
	date, _ := h.todayKeys()
	return h.scalar(`SELECT COALESCE((SELECT used FROM usage_daily WHERE user_id = ? AND usage_date = ?), 0)`, userID, date)
}

func (h *harness) providerCalls() int {
	h.provider.mu.Lock()
	defer h.provider.mu.Unlock()
	return h.provider.calls
}

func screenshotPNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestScreenshotReplyOfAShortChatCostsOneReply(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("shot-one@example.com")

	quote := h.screenshotEstimate(s.access, screenshotBody(nil))
	mustStatus(t, quote, http.StatusOK, "")
	if quote.num("estimated_units") != 1 || quote.num("max_units") != 1 || quote.num("available_units") != 7 ||
		quote.str("policy_version") != "screenshot-credits-v1" || quote.num("reference_tokens") != 2220 {
		t.Fatalf("estimate: %s", quote.raw)
	}
	if h.providerCalls() != 0 {
		t.Fatal("an estimate never reaches the provider")
	}

	res := h.screenshotReply(s.access, screenshotBody(map[string]any{
		"instruction": "Say yes, I will be available after 7 PM."}), "shot-one-0001")
	mustStatus(t, res, http.StatusOK, "")
	if res.str("reply") == "" || res.num("units") != 1 || res.num("refunded_units") != 0 || res.num("usage", "remaining_today") != 6 {
		t.Fatalf("reply: %s", res.raw)
	}
	user, version := h.lastPrompt()
	if version != "screenshot_text_v1" || !strings.Contains(user, "[3] Them ← REPLY TO THIS:") ||
		!strings.Contains(user, "Say yes, I will be available after 7 PM.") {
		t.Fatalf("prompt %s:\n%s", version, user)
	}
	if h.usedToday(s.userID) != 1 {
		t.Fatalf("used today = %d", h.usedToday(s.userID))
	}
	if n := h.scalar(`SELECT COUNT(*) FROM ai_usage_events WHERE user_id = ? AND mode = 'screenshot' AND status = 'success'
		AND units = 1 AND max_units = 1 AND credit_policy = 'screenshot-credits-v1'`, s.userID); n != 1 {
		t.Fatalf("usage event rows = %d", n)
	}
	if h.dbContains(screenshotSecret) || h.dbContains("available after 7 PM") {
		t.Fatal("the conversation or the instruction reached the database")
	}
}

func TestScreenshotReplyAsksBeforeChargingMoreAndChargesRealTokens(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("shot-more@example.com")
	body := screenshotBody(map[string]any{"conversation": longConversation()})

	quote := h.screenshotEstimate(s.access, body)
	mustStatus(t, quote, http.StatusOK, "")
	if quote.num("max_units") != 2 {
		t.Fatalf("a long chat may cost two: %s", quote.raw)
	}

	ask := h.screenshotReply(s.access, body, "shot-more-0001")
	mustStatus(t, ask, http.StatusConflict, "CREDIT_CONFIRMATION_REQUIRED")
	if mapAt(ask, "error", "details")["max_units"] != float64(2) || h.usedToday(s.userID) != 0 || h.providerCalls() != 0 {
		t.Fatalf("nothing is charged or sent before approval: %s", ask.raw)
	}

	// Approved two; the reply really used about one standard reply.
	h.provider.usage(1600, 100)
	body["max_units"] = 2
	cheap := h.screenshotReply(s.access, body, "shot-more-0002")
	mustStatus(t, cheap, http.StatusOK, "")
	if cheap.num("units") != 1 || cheap.num("refunded_units") != 1 || h.usedToday(s.userID) != 1 {
		t.Fatalf("charged by real tokens: %s (used %d)", cheap.raw, h.usedToday(s.userID))
	}

	// Now it really used more than 1.5 replies: two are charged.
	h.provider.usage(3400, 160)
	dear := h.screenshotReply(s.access, body, "shot-more-0003")
	mustStatus(t, dear, http.StatusOK, "")
	if dear.num("units") != 2 || dear.num("refunded_units") != 0 || h.usedToday(s.userID) != 3 {
		t.Fatalf("two charged: %s (used %d)", dear.raw, h.usedToday(s.userID))
	}

	// Far more than approved: never above the approved maximum.
	h.provider.usage(40000, 400)
	capped := h.screenshotReply(s.access, body, "shot-more-0004")
	mustStatus(t, capped, http.StatusOK, "")
	if capped.num("units") != 2 || h.usedToday(s.userID) != 5 {
		t.Fatalf("approved maximum is the ceiling: %s (used %d)", capped.raw, h.usedToday(s.userID))
	}
	if n := h.scalar(`SELECT COUNT(*) FROM usage_reservations WHERE user_id = ? AND mode = 'screenshot_extra' AND status = 'refunded'`, s.userID); n != 1 {
		t.Fatalf("refunded extra units = %d, want 1", n)
	}
}

func TestScreenshotReplyRefusesWhenTheBalanceCannotPayTheMaximum(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("shot-poor@example.com")
	date, month := h.todayKeys()
	h.exec(`INSERT INTO usage_daily (user_id, usage_date, used, updated_at) VALUES (?, ?, 6, 0)`, s.userID, date)
	h.exec(`INSERT INTO usage_monthly (user_id, usage_month, used, updated_at) VALUES (?, ?, 6, 0)`, s.userID, month)

	res := h.screenshotReply(s.access, screenshotBody(map[string]any{"conversation": longConversation(), "max_units": 2}), "shot-poor-0001")
	mustStatus(t, res, http.StatusPaymentRequired, "INSUFFICIENT_CREDITS")
	details := mapAt(res, "error", "details")
	if details["max_units"] != float64(2) || details["available_units"] != float64(1) || details["can_buy"] == nil {
		t.Fatalf("details: %s", res.raw)
	}
	if h.usedToday(s.userID) != 6 || h.providerCalls() != 0 {
		t.Fatalf("the first unit went back: used %d, calls %d", h.usedToday(s.userID), h.providerCalls())
	}
	// A short chat still fits the last reply.
	ok := h.screenshotReply(s.access, screenshotBody(nil), "shot-poor-0002")
	mustStatus(t, ok, http.StatusOK, "")
}

func TestScreenshotReplyRefundsEverythingWhenTheProviderFails(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("shot-fail@example.com")
	h.provider.mu.Lock()
	h.provider.err = domain.ErrProviderTimeout
	h.provider.mu.Unlock()
	res := h.screenshotReply(s.access, screenshotBody(map[string]any{"conversation": longConversation(), "max_units": 2}), "shot-fail-0001")
	mustStatus(t, res, http.StatusGatewayTimeout, "AI_TIMEOUT")
	if h.usedToday(s.userID) != 0 {
		t.Fatalf("a failed reply costs nothing, used %d", h.usedToday(s.userID))
	}
	if n := h.scalar(`SELECT COUNT(*) FROM usage_reservations WHERE user_id = ? AND status <> 'refunded'`, s.userID); n != 0 {
		t.Fatalf("%d reservations not refunded", n)
	}
}

func TestScreenshotReplyKeyIsChargedOnce(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("shot-key@example.com")
	first := h.screenshotReply(s.access, screenshotBody(nil), "shot-key-0001")
	again := h.screenshotReply(s.access, screenshotBody(nil), "shot-key-0001")
	mustStatus(t, first, http.StatusOK, "")
	mustStatus(t, again, http.StatusOK, "")
	if first.num("units") != 1 || again.num("units") != 0 || h.usedToday(s.userID) != 1 {
		t.Fatalf("one action, one charge: %s / %s", first.raw, again.raw)
	}
	other := h.replyTo(s.access, nil, map[string]string{"Idempotency-Key": "shot-key-0001"})
	mustStatus(t, other, http.StatusConflict, "CONFLICT")
}

func TestScreenshotImageModeNeedsConsentAndAValidImage(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("shot-image@example.com")
	data := screenshotPNG(t, 300, 640)
	image := map[string]any{"data": data, "width": 300, "height": 640}

	estimate := h.screenshotEstimate(s.access, map[string]any{"mode": "image", "image": map[string]any{"width": 1179, "height": 2556}})
	mustStatus(t, estimate, http.StatusOK, "")
	if estimate.num("image_tokens") != 85+170*8 || estimate.num("max_units") != 2 {
		t.Fatalf("an iPhone screenshot is priced by its size: %s", estimate.raw)
	}

	noConsent := h.screenshotReply(s.access, map[string]any{"mode": "image", "image": image}, "shot-image-0001")
	mustStatus(t, noConsent, http.StatusForbidden, "SCREENSHOT_CONSENT_REQUIRED")
	bad := h.screenshotReply(s.access, map[string]any{"mode": "image", "image_consent": legal.ScreenshotConsentVersion,
		"image": map[string]any{"data": base64.StdEncoding.EncodeToString([]byte("not an image at all"))}}, "shot-image-0002")
	mustStatus(t, bad, http.StatusBadRequest, "INVALID_REQUEST")
	if h.providerCalls() != 0 {
		t.Fatal("nothing reaches the provider without consent and a valid image")
	}

	h.provider.mu.Lock()
	h.provider.countTokens = 1900
	h.provider.mu.Unlock()
	res := h.screenshotReply(s.access, map[string]any{"mode": "image", "image_consent": legal.ScreenshotConsentVersion,
		"image": image, "max_units": 2, "language": "en"}, "shot-image-0003")
	mustStatus(t, res, http.StatusOK, "")
	if res.num("units") != 1 || mapAt(res, "quote")["tokens_counted"] != true || res.num("quote", "input_tokens") != 1900 {
		t.Fatalf("image reply: %s", res.raw)
	}
	h.provider.mu.Lock()
	last := h.provider.prompts[len(h.provider.prompts)-1]
	h.provider.mu.Unlock()
	if len(last.Images) != 1 || last.Images[0].MIME != "image/png" || last.Version != "screenshot_image_v1" {
		t.Fatalf("the image went to the provider: %d images, %s", len(last.Images), last.Version)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM ai_usage_events WHERE user_id = ? AND mode = 'screenshot_image' AND status = 'success'`, s.userID); n != 1 {
		t.Fatalf("image usage events = %d", n)
	}

	admin := h.signInAdmin()
	policy := h.adminCall(admin, http.MethodGet, "/settings", nil)
	current := mapAt(policy, "screenshot", "current")
	current["image_enabled"] = false
	mustStatus(t, h.adminCall(admin, http.MethodPost, "/settings/screenshot", current), http.StatusOK, "")
	off := h.screenshotReply(s.access, map[string]any{"mode": "image", "image_consent": legal.ScreenshotConsentVersion,
		"image": image, "max_units": 2}, "shot-image-0004")
	mustStatus(t, off, http.StatusForbidden, "SCREENSHOT_IMAGE_UNAVAILABLE")
	config := h.do(http.MethodGet, "/api/v1/config", nil, nil)
	if mapAt(config, "features")["screenshot_image"] != false || mapAt(config, "features")["screenshot_reply"] != true {
		t.Fatalf("config features: %s", config.raw)
	}
}

func TestScreenshotProPaysOneReplyWithADailyImageCap(t *testing.T) {
	h := newHarness(t, withEnv("TEST_APPROVED_ENTITLEMENTS", "true"))
	s := h.signIn("shot-pro@example.com")
	now := h.clock.Now()
	h.bankPro("shot-pro-1", s.userID, now.Add(-time.Hour), now.Add(30*24*time.Hour))

	h.provider.usage(40000, 400)
	text := h.screenshotReply(s.access, screenshotBody(map[string]any{"conversation": longConversation()}), "shot-pro-0001")
	mustStatus(t, text, http.StatusOK, "")
	if text.num("units") != 1 || mapAt(text, "quote")["unlimited"] != true {
		t.Fatalf("Pro pays one: %s", text.raw)
	}

	admin := h.signInAdmin()
	current := mapAt(h.adminCall(admin, http.MethodGet, "/settings", nil), "screenshot", "current")
	current["unlimited_images_per_day"] = 1
	mustStatus(t, h.adminCall(admin, http.MethodPost, "/settings/screenshot", current), http.StatusOK, "")

	image := map[string]any{"data": screenshotPNG(t, 200, 400)}
	body := map[string]any{"mode": "image", "image_consent": legal.ScreenshotConsentVersion, "image": image}
	mustStatus(t, h.screenshotReply(s.access, body, "shot-pro-0002"), http.StatusOK, "")
	capped := h.screenshotReply(s.access, body, "shot-pro-0003")
	mustStatus(t, capped, http.StatusTooManyRequests, "SCREENSHOT_IMAGE_LIMIT_REACHED")
	if mapAt(capped, "error", "details")["daily_limit"] != float64(1) {
		t.Fatalf("cap details: %s", capped.raw)
	}
}

func TestScreenshotPolicyIsAnAuditedAdminSetting(t *testing.T) {
	h := newHarness(t)
	admin := h.signInAdmin()
	settings := h.adminCall(admin, http.MethodGet, "/settings", nil)
	if settings.num("screenshot", "current", "ref_input_tokens") != 1500 || settings.str("screenshot", "policy_version") == "" ||
		mapAt(settings, "screenshot", "measured_30d", "reply") == nil {
		t.Fatalf("settings: %s", settings.raw)
	}
	current := mapAt(settings, "screenshot", "current")
	current["max_units"] = 0
	mustStatus(t, h.adminCall(admin, http.MethodPost, "/settings/screenshot", current), http.StatusBadRequest, "INVALID_REQUEST")
	current["max_units"] = 3
	current["shortcut_url"] = "https://evil.example/x"
	mustStatus(t, h.adminCall(admin, http.MethodPost, "/settings/screenshot", current), http.StatusBadRequest, "INVALID_REQUEST")
	current["shortcut_url"] = "https://www.icloud.com/shortcuts/abcdef0123456789"
	current["ref_input_tokens"] = 600
	saved := h.adminCall(admin, http.MethodPost, "/settings/screenshot", current)
	mustStatus(t, saved, http.StatusOK, "")
	if meta := h.auditMeta("settings.screenshot_policy.update"); meta == nil || meta["after"] == nil {
		t.Fatalf("the change is audited: %v", meta)
	}

	config := h.do(http.MethodGet, "/api/v1/config", nil, nil)
	shot := mapAt(config, "screenshot")
	if shot["shortcut_url"] != "https://www.icloud.com/shortcuts/abcdef0123456789" || shot["max_units"] != float64(3) ||
		shot["image_consent_version"] != legal.ScreenshotConsentVersion {
		t.Fatalf("config: %s", config.raw)
	}

	s := h.signIn("shot-admin@example.com")
	quote := h.screenshotEstimate(s.access, screenshotBody(map[string]any{"conversation": longConversation()}))
	// (≈3 525 + 4 × 180) / (600 + 4 × 180) ≈ 3.2 → 3, which is also the cap.
	if quote.num("max_units") != 3 || quote.num("reference_tokens") != 1320 {
		t.Fatalf("a smaller reference makes the same chat dearer, capped at 3: %s", quote.raw)
	}
}

func TestScreenshotRequestsAreValidated(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("shot-valid@example.com")
	empty := h.screenshotReply(s.access, map[string]any{"mode": "text", "conversation": map[string]any{"messages": []any{}}}, "")
	mustStatus(t, empty, http.StatusBadRequest, "INVALID_REQUEST")
	unknown := h.screenshotEstimate(s.access, map[string]any{"mode": "video"})
	mustStatus(t, unknown, http.StatusBadRequest, "INVALID_REQUEST")
	long := h.screenshotEstimate(s.access, map[string]any{"conversation": map[string]any{"messages": []any{
		map[string]any{"text": strings.Repeat("я", 1501)}}}})
	mustStatus(t, long, http.StatusBadRequest, "INVALID_REQUEST")
	if mapAt(long, "error", "details")["max"] != float64(1500) {
		t.Fatalf("limit details: %s", long.raw)
	}
	h.do(http.MethodDelete, "/api/v1/me/consents", nil, h.auth(s.access))
	withdrawn := h.screenshotEstimate(s.access, screenshotBody(nil))
	mustStatus(t, withdrawn, http.StatusForbidden, "CONSENT_REQUIRED")
}

// Two devices of one account at once, with room for only one of them: the
// reservation is all or none per request, so the balance never goes below
// zero and the refused request costs nothing.
func TestScreenshotRepliesFromTwoDevicesNeverOverspend(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("shot-race@example.com")
	date, month := h.todayKeys()
	h.exec(`INSERT INTO usage_daily (user_id, usage_date, used, updated_at) VALUES (?, ?, 4, 0)`, s.userID, date)
	h.exec(`INSERT INTO usage_monthly (user_id, usage_month, used, updated_at) VALUES (?, ?, 4, 0)`, s.userID, month)
	h.provider.usage(3400, 160) // each really costs two

	body := screenshotBody(map[string]any{"conversation": longConversation(), "max_units": 2})
	results := make(chan response, 2)
	for i := range 2 {
		go func() { results <- h.screenshotReply(s.access, body, "shot-race-000"+strconv.Itoa(i)) }()
	}
	statuses := map[int]int{}
	for range 2 {
		res := <-results
		statuses[res.status]++
	}
	if statuses[http.StatusOK] != 1 || statuses[http.StatusPaymentRequired] != 1 {
		t.Fatalf("statuses %v, want one reply and one refusal", statuses)
	}
	if used := h.usedToday(s.userID); used != 6 {
		t.Fatalf("used %d of 7, want 6: the refusal must give its first unit back", used)
	}
}
