package apptest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/email"
)

// fakeMailer — жіберілген хаттарды жадта ұстайды. Тесттер нақты хат жібермейді.
type fakeMailer struct {
	mu   sync.Mutex
	sent []email.OTPMessage
	fail error
}

func (f *fakeMailer) SendOTP(_ context.Context, msg email.OTPMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakeMailer) setFailure(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = err
}

func (f *fakeMailer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeMailer) last(t *testing.T) email.OTPMessage {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		t.Fatal("no e-mail was sent")
	}
	return f.sent[len(f.sent)-1]
}

// codeSequence — NewOTPCode-қа алдын ала белгілі кодтар береді
// (crypto/rand.Int 0..9999 үшін екі байт оқиды).
type codeSequence struct {
	mu    sync.Mutex
	codes []int
}

func (c *codeSequence) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(p) != 2 || len(c.codes) == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	next := c.codes[0]
	c.codes = c.codes[1:]
	p[0], p[1] = byte(next>>8), byte(next)
	return 2, nil
}

// withMailer — харнесске жалған пошта жеткізушісі және (берілсе) белгілі кодтар.
func withMailer(h *harness, codes ...int) *fakeMailer {
	mailer := &fakeMailer{}
	h.authSvc.WithEmailSender(mailer)
	if len(codes) > 0 {
		h.authSvc.WithRandom(&codeSequence{codes: codes})
	}
	return mailer
}

var device = map[string]any{"platform": "ios", "app_version": "2.0.0", "os_version": "18.2", "locale": "ru"}

func (h *harness) requestEmailCode(address string, headers ...map[string]string) response {
	var extra map[string]string
	if len(headers) > 0 {
		extra = headers[0]
	}
	return h.do(http.MethodPost, "/api/v1/auth/email/otp/request",
		map[string]any{"email": address, "locale": "ru"}, extra)
}

func (h *harness) verifyEmailCode(address, code string) response {
	return h.do(http.MethodPost, "/api/v1/auth/email/otp/verify",
		map[string]any{"email": address, "code": code, "device": device}, nil)
}

func mustStatus(t *testing.T, res response, status int, code string) {
	t.Helper()
	if res.status != status || res.errorCode() != code {
		t.Fatalf("got %d %q, want %d %q (body %s)", res.status, res.errorCode(), status, code, res.raw)
	}
}

// Flow A + B: жаңа пошта тіркелгі ашады, сол пошта қайта кіргенде сол тіркелгі табылады.
func TestEmailOTPCreatesThenFindsTheAccount(t *testing.T) {
	h := newHarness(t)
	mailer := withMailer(h)

	res := h.requestEmailCode("  New.User@Example.COM ")
	mustStatus(t, res, http.StatusOK, "")
	if res.str("masked_email") != "n******r@example.com" || res.num("resend_after") != 32 ||
		res.num("expires_in") != 300 || res.num("code_length") != 4 {
		t.Fatalf("challenge = %s", res.raw)
	}
	if _, demo := res.body["demo_mode"]; demo {
		t.Fatal("a real delivery is not demo mode")
	}
	msg := mailer.last(t)
	if msg.To != "new.user@example.com" || msg.Locale != "ru" || msg.TTL != 5*time.Minute || msg.Reference == "" {
		t.Fatalf("message = %+v", msg)
	}
	if !regexp.MustCompile(`^\d{4}$`).MatchString(msg.Code) {
		t.Fatalf("code %q is not four digits", msg.Code)
	}

	first := h.verifyEmailCode("NEW.USER@example.com", msg.Code)
	mustStatus(t, first, http.StatusOK, "")
	if isNew, _ := first.body["is_new_user"].(bool); !isNew {
		t.Fatal("the first verification must create the account")
	}
	if first.str("access_token") == "" || first.str("refresh_token") == "" {
		t.Fatal("tokens missing")
	}
	if first.str("user", "email") != "new.user@example.com" {
		t.Fatalf("email = %q", first.str("user", "email"))
	}
	if providers, _ := first.body["user"].(map[string]any)["auth_providers"].([]any); len(providers) != 1 || providers[0] != "email" {
		t.Fatalf("auth_providers = %v", providers)
	}
	if first.str("subscription", "plan", "code") != "free" {
		t.Fatal("a new account starts on the free plan")
	}

	h.clock.Advance(33 * time.Second)
	mustStatus(t, h.requestEmailCode("new.user@example.com"), http.StatusOK, "")
	again := h.verifyEmailCode("new.user@example.com", mailer.last(t).Code)
	mustStatus(t, again, http.StatusOK, "")
	if isNew, _ := again.body["is_new_user"].(bool); isNew || again.str("user", "id") != first.str("user", "id") {
		t.Fatalf("returning user got a different account: %s", again.raw)
	}
}

// Flow C: 32 секундтық күту серверде; жаңа код ескісін жояды.
func TestEmailOTPResendCooldownAndReplacement(t *testing.T) {
	h := newHarness(t)
	withMailer(h, 1234, 5678)
	address := "cooldown@example.com"

	mustStatus(t, h.requestEmailCode(address), http.StatusOK, "")

	early := h.requestEmailCode(address)
	mustStatus(t, early, http.StatusTooManyRequests, "OTP_RESEND_COOLDOWN")
	if early.num("error", "details", "retry_after_seconds") != 32 || early.header.Get("Retry-After") != "32" {
		t.Fatalf("cooldown details: %s / Retry-After %q", early.raw, early.header.Get("Retry-After"))
	}
	h.clock.Advance(31 * time.Second)
	stillEarly := h.requestEmailCode(address)
	mustStatus(t, stillEarly, http.StatusTooManyRequests, "OTP_RESEND_COOLDOWN")
	if stillEarly.num("error", "details", "retry_after_seconds") != 1 {
		t.Fatalf("one second left, got %s", stillEarly.raw)
	}

	h.clock.Advance(time.Second)
	mustStatus(t, h.requestEmailCode(address), http.StatusOK, "")

	mustStatus(t, h.verifyEmailCode(address, "1234"), http.StatusBadRequest, "INVALID_OTP")
	mustStatus(t, h.verifyEmailCode(address, "5678"), http.StatusOK, "")
	mustStatus(t, h.verifyEmailCode(address, "5678"), http.StatusBadRequest, "OTP_ALREADY_USED")
}

func TestEmailOTPExpiresAfterFiveMinutes(t *testing.T) {
	h := newHarness(t)
	withMailer(h, 4321, 4321)
	address := "late@example.com"

	mustStatus(t, h.requestEmailCode(address), http.StatusOK, "")
	h.clock.Advance(5 * time.Minute)
	mustStatus(t, h.verifyEmailCode(address, "4321"), http.StatusBadRequest, "OTP_EXPIRED")
	mustStatus(t, h.verifyEmailCode(address, "4321"), http.StatusBadRequest, "OTP_EXPIRED")

	// Just inside the window it works.
	mustStatus(t, h.requestEmailCode(address), http.StatusOK, "")
	h.clock.Advance(5*time.Minute - time.Second)
	mustStatus(t, h.verifyEmailCode(address, "4321"), http.StatusOK, "")
}

func TestEmailOTPAttemptLimit(t *testing.T) {
	h := newHarness(t)
	withMailer(h, 2468, 1357)
	address := "guess@example.com"
	mustStatus(t, h.requestEmailCode(address), http.StatusOK, "")

	for remaining := 4; remaining >= 1; remaining-- {
		res := h.verifyEmailCode(address, "0000")
		mustStatus(t, res, http.StatusBadRequest, "INVALID_OTP")
		if res.num("error", "details", "attempts_remaining") != float64(remaining) {
			t.Fatalf("attempts_remaining = %v, want %d", res.num("error", "details", "attempts_remaining"), remaining)
		}
	}
	mustStatus(t, h.verifyEmailCode(address, "0000"), http.StatusTooManyRequests, "OTP_ATTEMPTS_EXCEEDED")
	// The right code no longer helps: the code is burnt.
	mustStatus(t, h.verifyEmailCode(address, "2468"), http.StatusTooManyRequests, "OTP_ATTEMPTS_EXCEEDED")

	// Malformed input is rejected without spending an attempt.
	h.clock.Advance(33 * time.Second)
	mustStatus(t, h.requestEmailCode(address), http.StatusOK, "")
	for _, bad := range []string{"12a4", "123", "12345", ""} {
		mustStatus(t, h.verifyEmailCode(address, bad), http.StatusBadRequest, "INVALID_OTP")
	}
	for i := 0; i < 4; i++ {
		h.verifyEmailCode(address, "9999")
	}
	mustStatus(t, h.verifyEmailCode(address, "1357"), http.StatusOK, "")
}

// Бір кодпен екі параллель сұраныс екі сессия аша алмайды.
func TestEmailOTPConcurrentVerificationSucceedsOnce(t *testing.T) {
	h := newHarness(t)
	withMailer(h, 8642)
	address := "race@example.com"
	mustStatus(t, h.requestEmailCode(address), http.StatusOK, "")

	const workers = 12
	results := make([]response, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = h.verifyEmailCode(address, "8642")
		}(i)
	}
	close(start)
	wg.Wait()

	successes := 0
	for _, res := range results {
		switch {
		case res.status == http.StatusOK:
			successes++
		case res.errorCode() == "OTP_ALREADY_USED" || res.errorCode() == "OTP_ATTEMPTS_EXCEEDED":
		default:
			t.Fatalf("unexpected result %d %s", res.status, res.raw)
		}
	}
	if successes != 1 {
		t.Fatalf("%d sessions opened with one code, want 1", successes)
	}
	var sessions int
	if err := h.db.Reader().QueryRow(`SELECT COUNT(*) FROM refresh_tokens rt JOIN users u ON u.id = rt.user_id
		WHERE u.email = ?`, address).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatalf("refresh tokens = %d (%v), want 1", sessions, err)
	}
}

// Бір поштаға сағаттық шек IP-ге қарамай сақталады (IP шегінен бөлек баптау).
func TestEmailOTPPerAddressHourlyLimit(t *testing.T) {
	h := newHarness(t, withEnv("RATE_OTP_REQUEST_PER_ADDRESS_PER_HOUR", "3"), withEnv("TRUST_PROXY", "true"))
	withMailer(h)
	address := "limited@example.com"
	for i := 1; i <= 3; i++ {
		ip := map[string]string{"X-Forwarded-For": "10.0.0." + string(rune('0'+i))}
		mustStatus(t, h.requestEmailCode(address, ip), http.StatusOK, "")
		h.clock.Advance(33 * time.Second)
	}
	blocked := h.requestEmailCode(address, map[string]string{"X-Forwarded-For": "10.0.0.9"})
	mustStatus(t, blocked, http.StatusTooManyRequests, "RATE_LIMITED")
	if got := blocked.num("error", "details", "retry_after_seconds"); got != float64(3600-99) {
		t.Fatalf("retry_after_seconds = %v, want %d", got, 3600-99)
	}
	// Another address from the same network is unaffected.
	mustStatus(t, h.requestEmailCode("other@example.com", map[string]string{"X-Forwarded-For": "10.0.0.8"}),
		http.StatusOK, "")
}

func TestEmailOTPPerAddressDailyLimit(t *testing.T) {
	h := newHarness(t, withEnv("RATE_OTP_REQUEST_PER_DAY", "4"))
	withMailer(h)
	address := "daily@example.com"
	for i := 0; i < 4; i++ {
		mustStatus(t, h.requestEmailCode(address), http.StatusOK, "")
		h.clock.Advance(20 * time.Minute)
	}
	blocked := h.requestEmailCode(address)
	mustStatus(t, blocked, http.StatusTooManyRequests, "RATE_LIMITED")
	if got := blocked.num("error", "details", "retry_after_seconds"); got != float64(24*3600-80*60) {
		t.Fatalf("retry_after_seconds = %v", got)
	}
}

func TestEmailOTPPerIPLimit(t *testing.T) {
	h := newHarness(t, withEnv("RATE_OTP_REQUEST_PER_HOUR", "2"))
	withMailer(h)
	mustStatus(t, h.requestEmailCode("a@example.com"), http.StatusOK, "")
	mustStatus(t, h.requestEmailCode("b@example.com"), http.StatusOK, "")
	mustStatus(t, h.requestEmailCode("c@example.com"), http.StatusTooManyRequests, "RATE_LIMITED")
}

// Resend істемей қалса: 503, код жарамсыз, қайта сұрауға бірден болады.
func TestEmailOTPDeliveryFailure(t *testing.T) {
	h := newHarness(t)
	mailer := withMailer(h, 1111, 2222)
	mailer.setFailure(errors.New("resend: responded 503"))
	address := "outage@example.com"

	mustStatus(t, h.requestEmailCode(address), http.StatusServiceUnavailable, "EMAIL_DELIVERY_FAILED")
	mustStatus(t, h.verifyEmailCode(address, "1111"), http.StatusBadRequest, "INVALID_OTP")

	mailer.setFailure(nil)
	mustStatus(t, h.requestEmailCode(address), http.StatusOK, "")
	mustStatus(t, h.verifyEmailCode(address, "2222"), http.StatusOK, "")
}

func TestEmailOTPRejectsInvalidAddresses(t *testing.T) {
	h := newHarness(t)
	mailer := withMailer(h)
	for _, address := range []string{"", "no-at-sign", "user@", "Name <user@example.com>", "user@localhost"} {
		mustStatus(t, h.requestEmailCode(address), http.StatusBadRequest, "INVALID_EMAIL")
		mustStatus(t, h.verifyEmailCode(address, "1234"), http.StatusBadRequest, "INVALID_EMAIL")
	}
	if mailer.count() != 0 {
		t.Fatal("nothing may be sent to an invalid address")
	}
}

// Бар және жоқ тіркелгі үшін жауап бірдей: пошта арқылы тіркелгіні анықтау мүмкін емес.
func TestEmailOTPResponseDoesNotRevealAccounts(t *testing.T) {
	h := newHarness(t)
	mailer := withMailer(h)
	mustStatus(t, h.requestEmailCode("known@example.com"), http.StatusOK, "")
	mustStatus(t, h.verifyEmailCode("known@example.com", mailer.last(t).Code), http.StatusOK, "")
	h.clock.Advance(time.Minute)

	known := h.requestEmailCode("known@example.com")
	unknown := h.requestEmailCode("unknown@example.com")
	mustStatus(t, known, http.StatusOK, "")
	mustStatus(t, unknown, http.StatusOK, "")
	if len(known.body) != len(unknown.body) {
		t.Fatalf("shapes differ: %s vs %s", known.raw, unknown.raw)
	}
	for key, value := range known.body {
		if key != "masked_email" && unknown.body[key] != value {
			t.Fatalf("%s differs: %v vs %v", key, value, unknown.body[key])
		}
	}
}

// Код ашық түрде сақталмайды және журналға жазылмайды.
func TestEmailOTPIsNeverStoredOrLoggedInClear(t *testing.T) {
	h := newHarness(t)
	withMailer(h, 7351)
	address := "private@example.com"
	mustStatus(t, h.requestEmailCode(address), http.StatusOK, "")

	var hash string
	if err := h.db.Reader().QueryRow(`SELECT code_hash FROM otp_codes WHERE identity_value = ?`, address).
		Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "h1:") || strings.Contains(hash, "7351") {
		t.Fatalf("stored hash %q", hash)
	}
	mustStatus(t, h.verifyEmailCode(address, "7351"), http.StatusOK, "")
	logs := h.logs.String()
	if strings.Contains(logs, "7351") || strings.Contains(logs, address) {
		t.Fatal("the code or the address reached the logs")
	}
}

// Ескі build-тер /auth/request-otp арқылы поштаға келсе де, сол жаңа ағын жұмыс істейді.
func TestLegacyEndpointUsesTheEmailFlow(t *testing.T) {
	h := newHarness(t)
	mailer := withMailer(h)
	res := h.do(http.MethodPost, "/api/v1/auth/request-otp",
		map[string]any{"identifier": "legacy@example.com", "locale": "kk"}, nil)
	mustStatus(t, res, http.StatusOK, "")
	if res.str("kind") != "email" || res.str("channel") != "email" {
		t.Fatalf("challenge = %s", res.raw)
	}
	verify := h.do(http.MethodPost, "/api/v1/auth/verify-otp", map[string]any{
		"identifier": "legacy@example.com", "code": mailer.last(t).Code, "device": device,
	}, nil)
	mustStatus(t, verify, http.StatusOK, "")
}

// Провайдерсіз, демо режимде: демо код, ештеңе жіберілмейді.
func TestEmailOTPDemoModeWithoutProvider(t *testing.T) {
	h := newHarness(t)
	res := h.requestEmailCode("demo@example.com")
	mustStatus(t, res, http.StatusOK, "")
	if demo, _ := res.body["demo_mode"].(bool); !demo {
		t.Fatalf("demo mode must be reported: %s", res.raw)
	}
	mustStatus(t, h.verifyEmailCode("demo@example.com", "1111"), http.StatusOK, "")
}

// Телефонмен ашылған тіркелгі поштаны қосып, кейін сол поштамен кіре алады.
func TestPhoneAccountLinksEmailAndSignsInWithIt(t *testing.T) {
	h := newHarness(t)
	mailer := withMailer(h)
	phoneUser := h.signIn("+7 701 909 09 09")

	link := h.do(http.MethodPost, "/api/v1/me/email/otp/request",
		map[string]any{"email": "Owner@Example.com", "locale": "kk"}, h.auth(phoneUser.access))
	mustStatus(t, link, http.StatusOK, "")
	code := mailer.last(t).Code

	// A link code cannot sign anyone in.
	mustStatus(t, h.verifyEmailCode("owner@example.com", code), http.StatusBadRequest, "INVALID_OTP")

	linked := h.do(http.MethodPost, "/api/v1/me/email/otp/verify",
		map[string]any{"email": "owner@example.com", "code": code}, h.auth(phoneUser.access))
	mustStatus(t, linked, http.StatusOK, "")
	if linked.str("user", "email") != "owner@example.com" || linked.str("user", "phone") != "+77019090909" {
		t.Fatalf("linked = %s", linked.raw)
	}

	h.clock.Advance(time.Minute)
	mustStatus(t, h.requestEmailCode("owner@example.com"), http.StatusOK, "")
	session := h.verifyEmailCode("owner@example.com", mailer.last(t).Code)
	mustStatus(t, session, http.StatusOK, "")
	if session.str("user", "id") != phoneUser.userID {
		t.Fatal("signing in with the linked e-mail must reach the phone account")
	}

	// The account now has an address: linking another is refused up front.
	again := h.do(http.MethodPost, "/api/v1/me/email/otp/request",
		map[string]any{"email": "second@example.com"}, h.auth(phoneUser.access))
	mustStatus(t, again, http.StatusConflict, "CONFLICT")
}

func TestLinkingAnAddressOfAnotherAccountIsRefused(t *testing.T) {
	h := newHarness(t)
	mailer := withMailer(h)
	mustStatus(t, h.requestEmailCode("taken@example.com"), http.StatusOK, "")
	mustStatus(t, h.verifyEmailCode("taken@example.com", mailer.last(t).Code), http.StatusOK, "")
	h.clock.Advance(time.Minute)

	phoneUser := h.signIn("+7 701 808 08 08")
	mustStatus(t, h.do(http.MethodPost, "/api/v1/me/email/otp/request",
		map[string]any{"email": "taken@example.com"}, h.auth(phoneUser.access)), http.StatusOK, "")
	res := h.do(http.MethodPost, "/api/v1/me/email/otp/verify",
		map[string]any{"email": "taken@example.com", "code": mailer.last(t).Code}, h.auth(phoneUser.access))
	mustStatus(t, res, http.StatusConflict, "EMAIL_ALREADY_IN_USE")
}
