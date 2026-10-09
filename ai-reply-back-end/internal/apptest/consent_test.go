package apptest

import (
	"net/http"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/legal"
)

// signInWithoutConsent — поштамен кіреді, бірақ шарттарды әлі қабылдамайды.
func (h *harness) signInWithoutConsent(address string) session {
	h.t.Helper()
	mustStatus(h.t, h.requestEmailCode(address), http.StatusOK, "")
	verify := h.verifyEmailCode(address, "1111")
	if verify.status != http.StatusOK {
		h.t.Fatalf("verify: %d %s", verify.status, verify.raw)
	}
	return session{access: verify.str("access_token"), refresh: verify.str("refresh_token"),
		userID: verify.str("user", "id")}
}

// aiCalls — үш AI эндпоинті: әрқайсысы келісімсіз тоқтауы керек.
func (h *harness) aiCalls(access string) map[string]response {
	return map[string]response{
		"reply":   h.generate(access, "Сәлем, бағасы қанша?"),
		"compose": h.compose(access, map[string]any{"instruction": "Әріптесімді туған күнімен құттықта", "language": "kk"}),
		"polish":  h.polish(access, map[string]any{"text": "ертең кездесейік па", "input_language": "kk"}),
	}
}

func TestAIRefusedWithoutConsent(t *testing.T) {
	h := newHarness(t)
	s := h.signInWithoutConsent("noconsent@example.com")

	for name, res := range h.aiCalls(s.access) {
		if res.status != http.StatusForbidden || res.errorCode() != "CONSENT_REQUIRED" {
			t.Fatalf("%s without consent: %d %s", name, res.status, res.raw)
		}
	}
	if h.provider.calls != 0 {
		t.Fatalf("provider called %d times without consent", h.provider.calls)
	}
	// Nothing was reserved either: the refusal comes before the quota.
	if used := h.entitlement(s.userID).UsedToday; used != 0 {
		t.Fatalf("used_today = %d", used)
	}
	if me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access)); me.body["legal_consent"] != nil {
		t.Fatalf("/me reports a consent that was never given: %s", me.raw)
	}

	h.consent(s.access)
	for name, res := range h.aiCalls(s.access) {
		if res.status != http.StatusOK {
			t.Fatalf("%s after consent: %d %s", name, res.status, res.raw)
		}
	}
}

// Шарттардың жаңа нұсқасы шыққанда ескі келісім жарамайды.
func TestAIRefusedAfterLegalVersionChange(t *testing.T) {
	h := newHarness(t)
	s := h.signInWithoutConsent("oldterms@example.com")
	// What an account that accepted the previous documents has in the database.
	if _, err := h.db.Writer().Exec(`INSERT INTO legal_consents (id, user_id, terms_version, privacy_version,
		accepted_at, locale, platform, app_version, created_at) VALUES ('c-old', ?, '2026-09-19', '2026-09-19',
		1000, 'kk', 'ios', '1.0.0', 1000)`, s.userID); err != nil {
		t.Fatal(err)
	}
	if legal.TermsVersion == "2026-09-19" || legal.PrivacyVersion == "2026-09-19" {
		t.Fatal("the test needs current versions that differ from the old row")
	}

	if res := h.generate(s.access, "Сәлем!"); res.status != http.StatusForbidden || res.errorCode() != "CONSENT_REQUIRED" {
		t.Fatalf("old consent accepted: %d %s", res.status, res.raw)
	}
	config := h.do(http.MethodGet, "/api/v1/config", nil, nil)
	if config.str("legal", "terms_version") != "2026-10-08" || config.str("legal", "privacy_version") != "2026-10-08" {
		t.Fatalf("legal versions = %s", config.raw)
	}

	h.consent(s.access)
	if res := h.generate(s.access, "Сәлем!"); res.status != http.StatusOK {
		t.Fatalf("after accepting the new version: %d %s", res.status, res.raw)
	}
}

// Келісімді кері қайтару: AI тоқтайды, тіркелгі мен жазба қалады, қайта келісім қалпына келтіреді.
func TestConsentWithdrawalStopsAIUntilAcceptedAgain(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 701 555 66 77")
	if res := h.generate(s.access, "Сәлем!"); res.status != http.StatusOK {
		t.Fatalf("before withdrawal: %d %s", res.status, res.raw)
	}

	if res := h.do(http.MethodDelete, "/api/v1/me/consents", nil, nil); res.status != http.StatusUnauthorized {
		t.Fatalf("withdrawal without a token: %d", res.status)
	}
	res := h.do(http.MethodDelete, "/api/v1/me/consents", nil, h.auth(s.access))
	if res.status != http.StatusOK || res.body["ok"] != true {
		t.Fatalf("withdraw: %d %s", res.status, res.raw)
	}
	// Repeating it is harmless.
	if again := h.do(http.MethodDelete, "/api/v1/me/consents", nil, h.auth(s.access)); again.status != http.StatusOK {
		t.Fatalf("second withdrawal: %d %s", again.status, again.raw)
	}

	calls := h.provider.calls
	for name, res := range h.aiCalls(s.access) {
		if res.status != http.StatusForbidden || res.errorCode() != "CONSENT_REQUIRED" {
			t.Fatalf("%s after withdrawal: %d %s", name, res.status, res.raw)
		}
	}
	if h.provider.calls != calls {
		t.Fatal("a withdrawn consent still reached the provider")
	}
	me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access))
	if me.status != http.StatusOK || me.body["legal_consent"] != nil {
		t.Fatalf("/me after withdrawal: %d %s", me.status, me.raw)
	}
	// The record stays, marked as withdrawn; the account stays too.
	if n := h.scalar(`SELECT COUNT(*) FROM legal_consents WHERE user_id = ? AND withdrawn_at IS NOT NULL`, s.userID); n != 1 {
		t.Fatalf("withdrawn rows = %d", n)
	}

	h.consent(s.access)
	if n := h.scalar(`SELECT COUNT(*) FROM legal_consents WHERE user_id = ? AND withdrawn_at IS NULL`, s.userID); n != 1 {
		t.Fatalf("active rows after accepting again = %d", n)
	}
	if res := h.generate(s.access, "Сәлем!"); res.status != http.StatusOK {
		t.Fatalf("after accepting again: %d %s", res.status, res.raw)
	}
}
