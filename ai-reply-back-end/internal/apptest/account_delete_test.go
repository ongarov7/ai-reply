package apptest

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// fakeAppleRevoker — Apple-ге сұраныс жібермейді, қандай код келгенін жазады.
type fakeAppleRevoker struct {
	mu    sync.Mutex
	codes []string
	err   error
}

func (f *fakeAppleRevoker) Revoke(_ context.Context, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes = append(f.codes, code)
	return f.err
}

// userTables — user_id бағаны бар әр кесте (схемадан оқылады: жаңа кесте өздігінен тексеріледі).
func (h *harness) userTables() []string {
	h.t.Helper()
	rows, err := h.db.Reader().Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		h.t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			h.t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	var out []string
	for _, table := range tables {
		if h.scalar(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'user_id'`, table) > 0 {
			out = append(out, table)
		}
	}
	sort.Strings(out)
	return out
}

// rowsByUser — әр кестеде осы қолданушының жолдары.
func (h *harness) rowsByUser(userID string) map[string]int {
	h.t.Helper()
	out := map[string]int{"users": h.scalar(`SELECT COUNT(*) FROM users WHERE id = ?`, userID)}
	for _, table := range h.userTables() {
		out[table] = h.scalar(`SELECT COUNT(*) FROM `+table+` WHERE user_id = ?`, userID)
	}
	return out
}

// fullAccount — әр кестеде ізі бар тіркелгі: телефон мен пошта, профиль, жауаптар,
// өнім оқиғалары, push орнатуы, баптаулар, хабарлама, төлем, шағым.
func (h *harness) fullAccount(phone, address string) session {
	h.t.Helper()
	s := h.signIn(phone)
	mustStatus(h.t, h.do(http.MethodPost, "/api/v1/me/email/otp/request",
		map[string]any{"email": address, "locale": "ru"}, h.auth(s.access)), http.StatusOK, "")
	mustStatus(h.t, h.do(http.MethodPost, "/api/v1/me/email/otp/verify",
		map[string]any{"email": address, "code": "1111"}, h.auth(s.access)), http.StatusOK, "")
	mustStatus(h.t, h.do(http.MethodPatch, "/api/v1/me", map[string]any{
		"display_name": "Айгерім", "role": "Флорист", "business_offering": "Гүлдер", "grammatical_gender": "female",
	}, h.auth(s.access)), http.StatusOK, "")
	mustStatus(h.t, h.generate(s.access, "Сәлеметсіз бе, гүл бар ма?"), http.StatusOK, "")
	mustStatus(h.t, h.sendEvents(s.access, map[string]any{"name": "keyboard_enabled_detected"}), http.StatusOK, "")
	h.mustRegister(installation(installID(71), domain.PlatformIOS, fcmToken(71)), s.access)
	mustStatus(h.t, h.do(http.MethodPut, "/api/v1/me/notification-preferences",
		map[string]any{"preferences": map[string]any{"marketing": true}}, h.auth(s.access)), http.StatusOK, "")
	h.notifyUser(s.userID, "account-deletion-test")
	h.buy(s, "standard", 1)
	mustStatus(h.t, h.report(s.access, map[string]any{"mode": "reply", "reason": "other", "text": "Жауап мәтіні"}),
		http.StatusCreated, "")
	return s
}

func TestAccountDeletionRemovesEveryUserRow(t *testing.T) {
	h := newHarness(t)
	s := h.fullAccount("+7 701 909 10 11", "aigerim.delete@example.com")
	other := h.signIn("+7 701 909 10 12")

	before := h.rowsByUser(s.userID)
	for _, table := range []string{"users", "user_profiles", "auth_identities", "devices", "refresh_tokens",
		"subscriptions", "usage_daily", "usage_monthly", "ai_usage_events", "payments", "legal_consents",
		"product_events", "app_installations", "notification_preferences", "notifications",
		"notification_deliveries", "ai_reports"} {
		if before[table] == 0 {
			t.Fatalf("%s has no row for the account before deletion — the test would prove nothing (%v)", table, before)
		}
	}
	if h.scalar(`SELECT COUNT(*) FROM otp_codes WHERE identity_value IN ('+77019091011', 'aigerim.delete@example.com')`) < 2 {
		t.Fatal("expected sign-in codes for the phone and the e-mail")
	}

	res := h.do(http.MethodDelete, "/api/v1/me", nil, h.auth(s.access))
	if res.status != http.StatusOK || res.body["deleted"] != true || res.body["apple_token_revoked"] != false {
		t.Fatalf("delete: %d %s", res.status, res.raw)
	}

	for table, n := range h.rowsByUser(s.userID) {
		if n != 0 {
			t.Errorf("%s still has %d row(s) of the deleted account", table, n)
		}
	}
	if n := h.scalar(`SELECT COUNT(*) FROM otp_codes WHERE identity_value IN ('+77019091011', 'aigerim.delete@example.com')`); n != 0 {
		t.Errorf("otp_codes keeps %d code(s) of the deleted account", n)
	}
	// The phone stays registered, anonymous and without a push token.
	if n := h.scalar(`SELECT COUNT(*) FROM app_installations WHERE installation_id = ? AND user_id IS NULL
		AND push_token_sealed IS NULL AND push_token_hash IS NULL AND push_status = 'none'`, installID(71)); n != 1 {
		t.Errorf("installation was not detached and cleared")
	}
	// Another account is untouched.
	if rows := h.rowsByUser(other.userID); rows["users"] != 1 || rows["legal_consents"] != 1 {
		t.Errorf("another account lost data: %v", rows)
	}

	if me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access)); me.status != http.StatusUnauthorized {
		t.Fatalf("access token after deletion: %d", me.status)
	}
	if refreshed := h.do(http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refresh_token": s.refresh}, nil); refreshed.status != http.StatusUnauthorized {
		t.Fatalf("refresh token after deletion: %d", refreshed.status)
	}
	if again := h.do(http.MethodDelete, "/api/v1/me", nil, h.auth(s.access)); again.status != http.StatusUnauthorized {
		t.Fatalf("second delete: %d", again.status)
	}

	logs := h.logs.String()
	if !strings.Contains(logs, `"msg":"account deleted"`) || !strings.Contains(logs, s.userID) {
		t.Fatal("no metadata log line for the deletion")
	}
	if strings.Contains(logs, "aigerim.delete@example.com") {
		t.Fatal("logs contain the e-mail of the deleted account")
	}

	// Signing in with the same phone opens a brand-new, empty account.
	fresh := h.signIn("+7 701 909 10 11")
	if fresh.userID == s.userID || !fresh.isNew {
		t.Fatalf("sign-in after deletion: id %s new %v", fresh.userID, fresh.isNew)
	}
}

// POST /me/delete: Apple арқылы кірген тіркелгі, код бар — токен кері қайтарылады.
func TestAccountDeletionRevokesTheAppleToken(t *testing.T) {
	h := newHarness(t)
	idp := withIdentityProviders(t, h)
	revoker := &fakeAppleRevoker{}
	h.users.WithAppleRevoker(revoker)

	signIn := h.appleSignIn(idp.appleToken("001234.delete.0001", "dana@icloud.com", "n-1", nil), "n-1", "")
	mustStatus(t, signIn, http.StatusOK, "")
	access, userID := signIn.str("access_token"), signIn.str("user", "id")

	res := h.do(http.MethodPost, "/api/v1/me/delete", map[string]any{"apple_authorization_code": "c-apple-1"}, h.auth(access))
	if res.status != http.StatusOK || res.body["deleted"] != true || res.body["apple_token_revoked"] != true {
		t.Fatalf("delete: %d %s", res.status, res.raw)
	}
	if strings.Join(revoker.codes, ",") != "c-apple-1" {
		t.Fatalf("revoked codes = %v", revoker.codes)
	}
	if h.scalar(`SELECT COUNT(*) FROM users WHERE id = ?`, userID) != 0 {
		t.Fatal("account still exists")
	}
	if strings.Contains(h.logs.String(), "c-apple-1") {
		t.Fatal("logs contain the Apple authorization code")
	}
}

// Apple жауап бермесе де тіркелгі жойылады; нәтиже apple_token_revoked=false.
func TestAccountDeletionDoesNotWaitForApple(t *testing.T) {
	h := newHarness(t)
	idp := withIdentityProviders(t, h)
	revoker := &fakeAppleRevoker{err: errors.New("appleid: token revocation failed: /auth/token responded 400 invalid_grant")}
	h.users.WithAppleRevoker(revoker)

	signIn := h.appleSignIn(idp.appleToken("001234.delete.0002", "erlan@icloud.com", "n-2", nil), "n-2", "")
	mustStatus(t, signIn, http.StatusOK, "")
	res := h.do(http.MethodDelete, "/api/v1/me", map[string]any{"apple_authorization_code": "c-apple-2"},
		h.auth(signIn.str("access_token")))
	if res.status != http.StatusOK || res.body["deleted"] != true || res.body["apple_token_revoked"] != false {
		t.Fatalf("delete: %d %s", res.status, res.raw)
	}
	if h.scalar(`SELECT COUNT(*) FROM users WHERE id = ?`, signIn.str("user", "id")) != 0 {
		t.Fatal("a failed revocation kept the account")
	}
	if !strings.Contains(h.logs.String(), "apple token revocation failed") {
		t.Fatal("the failure is not logged")
	}

	// An account without Apple ignores the code; an unknown body field is refused.
	s := h.signIn("+7 701 909 10 13")
	if bad := h.do(http.MethodPost, "/api/v1/me/delete", map[string]any{"reason": "x"}, h.auth(s.access)); bad.status != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", bad.status)
	}
	res = h.do(http.MethodPost, "/api/v1/me/delete", map[string]any{"apple_authorization_code": "c-apple-3"}, h.auth(s.access))
	if res.status != http.StatusOK || res.body["apple_token_revoked"] != false || len(revoker.codes) != 1 {
		t.Fatalf("delete without apple: %d %s, codes %v", res.status, res.raw, revoker.codes)
	}
}

func (h *harness) requestDeletion(address string) response {
	h.t.Helper()
	res := h.do(http.MethodPost, "/api/v1/account/delete/request", map[string]any{"email": address, "locale": "kk"}, nil)
	h.authSvc.Wait()
	return res
}

func (h *harness) confirmDeletion(address, code string) response {
	return h.do(http.MethodPost, "/api/v1/account/delete/confirm", map[string]any{"email": address, "code": code}, nil)
}

// /account/delete беті: пошта → код → жою. Тіркелгі бар-жоғы жауаптан білінбейді.
func TestWebAccountDeletion(t *testing.T) {
	h := newHarness(t)
	signedIn := h.signInWithoutConsent("web.delete@example.com")
	h.clock.Advance(time.Minute) // past the sign-in code's resend cooldown
	mailer := withMailer(h, 4821, 1357)
	sent := mailer.count()

	// Unknown address: same answer, nothing sent, no code stored.
	unknown := h.requestDeletion("nobody@example.com")
	if unknown.status != http.StatusOK || unknown.body["ok"] != true {
		t.Fatalf("unknown address: %d %s", unknown.status, unknown.raw)
	}
	if mailer.count() != sent || h.scalar(`SELECT COUNT(*) FROM otp_codes WHERE identity_value = 'nobody@example.com'`) != 0 {
		t.Fatal("a code was issued for an address without an account")
	}
	mustStatus(t, h.confirmDeletion("nobody@example.com", "1234"), http.StatusBadRequest, "INVALID_OTP")
	mustStatus(t, h.do(http.MethodPost, "/api/v1/account/delete/request", map[string]any{"email": "not-an-email"}, nil),
		http.StatusBadRequest, "INVALID_EMAIL")

	// The account's own address gets a deletion code, worded as one.
	res := h.requestDeletion(" Web.Delete@Example.com ")
	if res.status != http.StatusOK || res.body["ok"] != true || len(res.body) != 1 {
		t.Fatalf("request: %d %s", res.status, res.raw)
	}
	msg := mailer.last(t)
	if mailer.count() != sent+1 || msg.To != "web.delete@example.com" || msg.Purpose != domain.OTPPurposeDelete || msg.Locale != "kk" {
		t.Fatalf("deletion e-mail = %+v (count %d)", msg, mailer.count())
	}

	// A wrong code changes nothing; a sign-in verification cannot use the deletion code.
	wrong := h.confirmDeletion("web.delete@example.com", "0000")
	mustStatus(t, wrong, http.StatusBadRequest, "INVALID_OTP")
	if wrong.num("error", "details", "attempts_remaining") != 4 {
		t.Fatalf("attempts = %s", wrong.raw)
	}
	mustStatus(t, h.verifyEmailCode("web.delete@example.com", msg.Code), http.StatusBadRequest, "INVALID_OTP")
	if h.scalar(`SELECT COUNT(*) FROM users WHERE id = ?`, signedIn.userID) != 1 {
		t.Fatal("the account went away on a wrong code")
	}

	done := h.confirmDeletion("web.delete@example.com", msg.Code)
	if done.status != http.StatusOK || done.body["deleted"] != true {
		t.Fatalf("confirm: %d %s", done.status, done.raw)
	}
	for table, n := range h.rowsByUser(signedIn.userID) {
		if n != 0 {
			t.Errorf("%s still has %d row(s)", table, n)
		}
	}
	if me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(signedIn.access)); me.status != http.StatusUnauthorized {
		t.Fatalf("token after web deletion: %d", me.status)
	}
	// The code is gone with the account.
	mustStatus(t, h.confirmDeletion("web.delete@example.com", msg.Code), http.StatusBadRequest, "INVALID_OTP")
	if strings.Contains(h.logs.String(), "web.delete@example.com") {
		t.Fatal("logs contain the address")
	}
}

// Бір поштаға жою кодын қайта сұрау — кіру кодымен бірдей шектеулер, бірақ жауап өзгермейді.
func TestWebAccountDeletionRequestsAreLimitedQuietly(t *testing.T) {
	h := newHarness(t)
	h.signInWithoutConsent("quiet@example.com")
	h.clock.Advance(time.Minute) // past the sign-in code's resend cooldown
	mailer := withMailer(h, 2222, 3333)
	sent := mailer.count()

	first := h.requestDeletion("quiet@example.com")
	second := h.requestDeletion("quiet@example.com") // inside the resend cooldown
	if first.status != http.StatusOK || second.status != http.StatusOK || string(first.raw) != string(second.raw) {
		t.Fatalf("answers differ: %s / %s", first.raw, second.raw)
	}
	if mailer.count() != sent+1 {
		t.Fatalf("e-mails sent = %d, want one (cooldown)", mailer.count()-sent)
	}
}

func TestAccountPagesRender(t *testing.T) {
	h := newHarness(t, withEnv("CONTACT_EMAIL", "Support@AI-Reply.kz"))
	for _, locale := range []string{"kk", "ru", "en", "uz"} {
		status, body := fetch(t, h, "/account/delete?lang="+locale)
		if status != http.StatusOK || !strings.Contains(body, `id="delete-account"`) ||
			!strings.Contains(body, "AI Reply") || strings.Contains(body, "delete.") {
			t.Fatalf("/account/delete %s: %d", locale, status)
		}
		status, body = fetch(t, h, "/support?lang="+locale)
		if status != http.StatusOK || !strings.Contains(body, "mailto:support@ai-reply.kz") ||
			!strings.Contains(body, "/account/delete") || strings.Contains(body, "support.") {
			t.Fatalf("/support %s: %d", locale, status)
		}
	}
	_, body := fetch(t, h, "/account/delete?lang=en")
	for _, needle := range []string{"Settings → Account → Delete account", "valid for 5 minutes", "/support?lang=en",
		"removed automatically after 180 days without use"} {
		if !strings.Contains(body, needle) {
			t.Fatalf("/account/delete is missing %q", needle)
		}
	}
	_, ru := fetch(t, h, "/account/delete?lang=ru")
	if !strings.Contains(ru, "Настройки → Аккаунт → Удалить аккаунт") || !strings.Contains(ru, "через 180 дней без использования") {
		t.Fatal("ru page does not name the in-app path or the installation retention")
	}

	// fetch does not follow redirects: the answer itself names the page.
	status, _ := fetch(t, h, "/delete-account?lang=ru")
	if status != http.StatusMovedPermanently {
		t.Fatalf("/delete-account: %d", status)
	}
	res, err := h.server.Client().Get(h.server.URL + "/delete-account?lang=ru")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if location := res.Header.Get("Location"); location != "/account/delete?lang=ru" {
		t.Fatalf("redirect target = %q", location)
	}

	// The footer links both pages everywhere; mailto only with an address.
	_, landing := fetch(t, h, "/?lang=en")
	if !strings.Contains(landing, `href="/support?lang=en"`) || !strings.Contains(landing, `href="/account/delete?lang=en"`) ||
		!strings.Contains(landing, "mailto:support@ai-reply.kz") {
		t.Fatal("footer links are missing")
	}
	bare := newHarness(t, withEnv("CONTACT_EMAIL", "")) // t.Setenv above lasts for the whole test
	_, landing = fetch(t, bare, "/?lang=en")
	_, support := fetch(t, bare, "/support?lang=en")
	if strings.Contains(landing, "mailto:") || strings.Contains(support, "mailto:") {
		t.Fatal("a mailto link without CONTACT_EMAIL")
	}
	if config := bare.do(http.MethodGet, "/api/v1/config", nil, nil); config.str("legal", "contact_email") != "" {
		t.Fatalf("contact_email = %q", config.str("legal", "contact_email"))
	}
}
