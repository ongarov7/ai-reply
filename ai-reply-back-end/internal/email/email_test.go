package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var texts = map[string]map[string]string{
	"en": {
		keySubject:    "AI Reply verification code",
		keyIntro:      "Your verification code is:",
		keyExpires:    "This code expires in {minutes} minutes.",
		keyNeverShare: "Do not share this code with anyone.",
		keyIgnore:     "If you did not request this code, you can ignore this email.",
	},
	"ru": {
		keySubject:    "Код подтверждения AI Reply",
		keyIntro:      "Ваш код подтверждения:",
		keyExpires:    "Код действует {minutes} мин.",
		keyNeverShare: "Никому не сообщайте этот код.",
		keyIgnore:     "Если вы не запрашивали код, просто проигнорируйте это письмо.",
	},
}

func translate(locale, key string) string {
	if v, ok := texts[locale][key]; ok {
		return v
	}
	return texts["en"][key]
}

func TestRenderOTPIsLocalizedAndComplete(t *testing.T) {
	content, err := RenderOTP(translate, "AI Reply", "ru-KZ", "0384", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if content.Subject != "Код подтверждения AI Reply" {
		t.Fatalf("subject = %q", content.Subject)
	}
	for _, part := range []string{"AI Reply", "Ваш код подтверждения:", "0384", "Код действует 5 мин.",
		"Никому не сообщайте этот код.", "проигнорируйте"} {
		if !strings.Contains(content.Text, part) || !strings.Contains(content.HTML, part) {
			t.Fatalf("%q missing from text or html", part)
		}
	}
	if !strings.Contains(content.HTML, `lang="ru"`) {
		t.Fatal("html lang must follow the locale")
	}

	// Unknown locales fall back to English; the leading zero survives.
	fallback, _ := RenderOTP(translate, "", "xx", "0007", 5*time.Minute)
	if fallback.Subject != "AI Reply verification code" || !strings.Contains(fallback.Text, "\n0007\n") {
		t.Fatalf("fallback = %+v", fallback)
	}
}

func TestRenderOTPEscapesHTML(t *testing.T) {
	hostile := func(_, key string) string {
		if key == keyIntro {
			return `<script>alert(1)</script>`
		}
		return translate("en", key)
	}
	content, err := RenderOTP(hostile, "AI <Reply>", "en", "1234", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content.HTML, "<script>") || strings.Contains(content.HTML, "AI <Reply>") {
		t.Fatal("translations and brand must be HTML-escaped")
	}
}

type resendStub struct {
	*httptest.Server
	hits     atomic.Int32
	statuses []int
	requests chan *capturedRequest
}

type capturedRequest struct {
	method, path, auth, idempotency, contentType string
	body                                         resendRequest
}

func newResendStub(t *testing.T, statuses ...int) *resendStub {
	s := &resendStub{statuses: statuses, requests: make(chan *capturedRequest, 8)}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(s.hits.Add(1))
		raw, _ := io.ReadAll(r.Body)
		captured := &capturedRequest{
			method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"),
			idempotency: r.Header.Get("Idempotency-Key"), contentType: r.Header.Get("Content-Type"),
		}
		_ = json.Unmarshal(raw, &captured.body)
		s.requests <- captured

		status := http.StatusOK
		if n <= len(s.statuses) {
			status = s.statuses[n-1]
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(`{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`))
			return
		}
		_, _ = fmt.Fprintf(w, `{"statusCode":%d,"name":"validation_error","message":"The ai-reply.kz domain is not verified."}`, status)
	}))
	t.Cleanup(s.Close)
	return s
}

func newTestResend(t *testing.T, url string) *Resend {
	t.Helper()
	sender, err := NewResend(ResendConfig{
		APIKey: "re_test_key", FromEmail: "noreply@ai-reply.kz", FromName: "AI Reply",
		BaseURL: url, Translate: translate,
	})
	if err != nil {
		t.Fatal(err)
	}
	return sender
}

func otp() OTPMessage {
	return OTPMessage{To: "user@example.com", Code: "0482", Locale: "en", TTL: 5 * time.Minute, Reference: "otp-row-1"}
}

func TestResendSendsTheDocumentedRequest(t *testing.T) {
	stub := newResendStub(t)
	if err := newTestResend(t, stub.URL).SendOTP(context.Background(), otp()); err != nil {
		t.Fatalf("send: %v", err)
	}
	req := <-stub.requests
	if req.method != http.MethodPost || req.path != "/emails" {
		t.Fatalf("%s %s, want POST /emails", req.method, req.path)
	}
	if req.auth != "Bearer re_test_key" || req.contentType != "application/json" {
		t.Fatalf("headers: auth=%q content-type=%q", req.auth, req.contentType)
	}
	if req.idempotency != "otp-otp-row-1" {
		t.Fatalf("idempotency key = %q", req.idempotency)
	}
	body := req.body
	if body.From != `"AI Reply" <noreply@ai-reply.kz>` {
		t.Fatalf("from = %q", body.From)
	}
	if len(body.To) != 1 || body.To[0] != "user@example.com" || body.Subject != "AI Reply verification code" {
		t.Fatalf("body = %+v", body)
	}
	if !strings.Contains(body.Text, "0482") || !strings.Contains(body.HTML, "0482") {
		t.Fatal("the code must be in both the text and the html part")
	}
	if body.Headers["X-Entity-Ref-ID"] != "otp-row-1" {
		t.Fatalf("headers = %v", body.Headers)
	}
}

func TestResendRetriesServerErrorsOnceWithTheSameKey(t *testing.T) {
	stub := newResendStub(t, http.StatusBadGateway)
	if err := newTestResend(t, stub.URL).SendOTP(context.Background(), otp()); err != nil {
		t.Fatalf("send after one 502: %v", err)
	}
	first, second := <-stub.requests, <-stub.requests
	if stub.hits.Load() != 2 || first.idempotency != second.idempotency || first.idempotency == "" {
		t.Fatalf("hits=%d keys=%q/%q", stub.hits.Load(), first.idempotency, second.idempotency)
	}
}

func TestResendDoesNotRetryRejectionsAndLeaksNothing(t *testing.T) {
	stub := newResendStub(t, http.StatusUnprocessableEntity)
	err := newTestResend(t, stub.URL).SendOTP(context.Background(), otp())
	var delivery *DeliveryError
	if !errors.As(err, &delivery) || delivery.Status != http.StatusUnprocessableEntity || delivery.Name != "validation_error" {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, ErrDelivery) {
		t.Fatal("a rejection must be ErrDelivery")
	}
	if stub.hits.Load() != 1 {
		t.Fatalf("a 4xx was retried: hits=%d", stub.hits.Load())
	}
	if msg := err.Error(); strings.Contains(msg, "re_test_key") || strings.Contains(msg, "0482") {
		t.Fatalf("error leaks a secret: %s", msg)
	}
}

func TestResendTransportFailure(t *testing.T) {
	stub := newResendStub(t)
	url := stub.URL
	stub.Close() // nothing listens any more
	err := newTestResend(t, url).SendOTP(context.Background(), otp())
	if !errors.Is(err, ErrDelivery) {
		t.Fatalf("err = %v, want ErrDelivery", err)
	}
	if strings.Contains(err.Error(), "re_test_key") {
		t.Fatal("transport error leaks the key")
	}
}

func TestResendStopsOnCancelledContext(t *testing.T) {
	stub := newResendStub(t, http.StatusServiceUnavailable, http.StatusServiceUnavailable)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := newTestResend(t, stub.URL).SendOTP(ctx, otp()); err == nil {
		t.Fatal("a cancelled context must not report success")
	}
}

func TestNewResendValidatesConfiguration(t *testing.T) {
	for name, cfg := range map[string]ResendConfig{
		"no key":         {FromEmail: "noreply@ai-reply.kz", Translate: translate},
		"no from":        {APIKey: "re_x", Translate: translate},
		"display name":   {APIKey: "re_x", FromEmail: "AI Reply <noreply@ai-reply.kz>", Translate: translate},
		"not an address": {APIKey: "re_x", FromEmail: "noreply", Translate: translate},
		"no translator":  {APIKey: "re_x", FromEmail: "noreply@ai-reply.kz"},
	} {
		if _, err := NewResend(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Хабарлама хаты OTP-пен бірдей жолмен кетеді, бірақ өз кілтімен және дайын мазмұнмен.
func TestResendSendsANotificationEmail(t *testing.T) {
	stub := newResendStub(t, http.StatusServiceUnavailable)
	msg := Outgoing{
		To:             "user@example.com",
		Content:        Content{Subject: "Тариф қосылды", Text: "Pro тарифі қосылды.", HTML: "<p>Pro тарифі қосылды.</p>"},
		IdempotencyKey: "notification-d-1",
		Reference:      "d-1",
	}
	if err := newTestResend(t, stub.URL).Send(context.Background(), msg); err != nil {
		t.Fatalf("send after one 503: %v", err)
	}
	first, second := <-stub.requests, <-stub.requests
	if first.idempotency != "notification-d-1" || second.idempotency != first.idempotency {
		t.Fatalf("idempotency keys = %q / %q", first.idempotency, second.idempotency)
	}
	body := second.body
	if body.Subject != "Тариф қосылды" || body.Text != msg.Content.Text || body.HTML != msg.Content.HTML ||
		len(body.To) != 1 || body.To[0] != "user@example.com" || body.Headers["X-Entity-Ref-ID"] != "d-1" {
		t.Fatalf("body = %+v", body)
	}
	if err := newTestResend(t, stub.URL).Send(context.Background(), Outgoing{Content: msg.Content}); err == nil {
		t.Fatal("a message without a recipient is refused before any request")
	}
}

func TestNotificationTemplates(t *testing.T) {
	keys := map[string]map[string]string{
		"ru": {
			"email.subscription_activated.subject": "Тариф {plan} подключён",
			"email.subscription_activated.body":    "Спасибо!\n\nТариф {plan} действует до {date}. <b>",
		},
		"en": {
			"email.subscription_activated.subject": "{plan} is active",
			"email.subscription_activated.body":    "Thank you.",
		},
	}
	translate := func(locale, key string) string {
		if v, ok := keys[locale][key]; ok {
			return v
		}
		if v, ok := keys["en"][key]; ok {
			return v
		}
		return key // localization.Bundle answers a missing key with the key itself
	}
	templates := NotificationTemplates{Translate: translate, Brand: "AI Reply"}
	content, err := templates.Render("subscription_activated", "ru-KZ", map[string]string{"plan": "Pro", "date": "01.11.2026"})
	if err != nil {
		t.Fatal(err)
	}
	if content.Subject != "Тариф Pro подключён" || !strings.Contains(content.Text, "Тариф Pro действует до 01.11.2026.") ||
		!strings.Contains(content.HTML, "Спасибо!") || !strings.Contains(content.HTML, `lang="ru"`) {
		t.Fatalf("content = %+v", content)
	}
	if strings.Contains(content.HTML, "<b>") {
		t.Fatal("texts must be HTML-escaped")
	}
	if english, err := templates.Render("subscription_activated", "uz", map[string]string{"plan": "Pro"}); err != nil ||
		english.Subject != "Pro is active" {
		t.Fatalf("fallback = %+v, %v", english, err)
	}
	if _, err := templates.Render("quota_low", "ru", nil); !errors.Is(err, ErrTemplateMissing) {
		t.Fatalf("a type without keys has no e-mail: %v", err)
	}
}

// Қосымша жолдар: сәлемдесу, мерзім (тек күн берілсе), қосымшадағы орны, түсініктеме.
func TestNotificationTemplatesOptionalLines(t *testing.T) {
	keys := map[string]string{
		"email.subscription_activated.subject":  "Тариф «{plan}» подключён",
		"email.subscription_activated.greeting": "Здравствуйте!",
		"email.subscription_activated.body":     "Тариф «{plan}» уже действует, дневной лимит ответов — {limit}.",
		"email.subscription_activated.expires":  "Тариф действует до {date}.",
		"email.subscription_activated.manage":   "Тариф можно посмотреть в приложении.",
		"email.subscription_activated.footer":   "Вы получили это письмо, потому что в аккаунте подключён тариф.",
	}
	translate := func(_, key string) string {
		if v, ok := keys[key]; ok {
			return v
		}
		return key
	}
	templates := NotificationTemplates{Translate: translate, Brand: "AI Reply"}
	params := map[string]string{"plan": "Pro <b>", "limit": "50", "date": "09.04.2026"}
	content, err := templates.Render("subscription_activated", "ru", params)
	if err != nil {
		t.Fatal(err)
	}
	want := "AI Reply\n\nТариф «Pro <b>» подключён\n\nЗдравствуйте!\n\nТариф «Pro <b>» уже действует, дневной лимит ответов — 50.\n\n" +
		"Тариф действует до 09.04.2026.\n\nТариф можно посмотреть в приложении.\n\n--\nВы получили это письмо, потому что в аккаунте подключён тариф.\n"
	if content.Text != want {
		t.Fatalf("text part:\n%s", content.Text)
	}
	for _, part := range []string{"Здравствуйте!", "до 09.04.2026.", "посмотреть в приложении", "потому что", "Pro &lt;b&gt;"} {
		if !strings.Contains(content.HTML, part) {
			t.Fatalf("%q missing from the html", part)
		}
	}
	if strings.Contains(content.HTML, "<b>") || strings.Contains(content.HTML, "{") {
		t.Fatal("values are escaped and every placeholder is filled")
	}

	// A plan without an end date: the expiry line is left out, nothing else changes.
	delete(params, "date")
	open, err := templates.Render("subscription_activated", "ru", params)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(open.Text, "действует до") || strings.Contains(open.HTML, "действует до") ||
		!strings.Contains(open.Text, "Здравствуйте!") {
		t.Fatalf("without a date:\n%s", open.Text)
	}

	// A value is never read as another placeholder.
	odd, err := templates.Render("subscription_activated", "ru", map[string]string{"plan": "{limit}", "limit": "7"})
	if err != nil || !strings.Contains(odd.Subject, "«{limit}»") {
		t.Fatalf("subject = %q, %v", odd.Subject, err)
	}

	// A required line without its value is an error, not an e-mail with "{plan}" in it.
	if _, err := templates.Render("subscription_activated", "ru", map[string]string{"limit": "7"}); err == nil ||
		errors.Is(err, ErrTemplateMissing) {
		t.Fatalf("missing plan: %v", err)
	}
}
