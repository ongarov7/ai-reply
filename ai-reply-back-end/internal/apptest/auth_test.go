package apptest

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

// Тіркелу: демо OTP 1111 қабылданады, жаңа қолданушы тегін тарифке түседі.
func TestDemoOTPRegistersUserOnFreePlan(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 701 123 45 67")

	if !session.isNew {
		t.Fatal("first sign-in should create a new user")
	}
	if session.access == "" || session.refresh == "" {
		t.Fatal("tokens missing")
	}

	me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(session.access))
	if me.status != http.StatusOK {
		t.Fatalf("me: status %d", me.status)
	}
	if code := me.str("subscription", "plan", "code"); code != "free" {
		t.Fatalf("plan = %q, want free", code)
	}
	if limit := me.num("usage", "daily_limit"); limit != 7 {
		t.Fatalf("daily limit = %v, want 7", limit)
	}
	if phone := me.str("user", "phone"); phone != "+77011234567" {
		t.Fatalf("phone = %q, want normalized E.164", phone)
	}

	// Қайта кіргенде жаңа есептік жазба жасалмайды.
	again := h.signIn("87011234567")
	if again.isNew {
		t.Fatal("second sign-in must reuse the account")
	}
	if again.userID != session.userID {
		t.Fatalf("user id changed: %s vs %s", again.userID, session.userID)
	}
}

// Қате код қабылданбайды, әрекеттер саны шектеулі.
func TestInvalidOTP(t *testing.T) {
	h := newHarness(t)
	identifier := "+7 707 000 11 22"
	h.do(http.MethodPost, "/api/v1/auth/request-otp", map[string]any{"identifier": identifier}, nil)

	res := h.do(http.MethodPost, "/api/v1/auth/verify-otp",
		map[string]any{"identifier": identifier, "code": "9999"}, nil)
	if res.status != http.StatusBadRequest || res.errorCode() != "INVALID_OTP" {
		t.Fatalf("got %d %s, want 400 INVALID_OTP", res.status, res.errorCode())
	}

	for i := 0; i < 5; i++ {
		h.do(http.MethodPost, "/api/v1/auth/verify-otp",
			map[string]any{"identifier": identifier, "code": "0000"}, nil)
	}
	blocked := h.do(http.MethodPost, "/api/v1/auth/verify-otp",
		map[string]any{"identifier": identifier, "code": "1111"}, nil)
	if blocked.errorCode() == "" || blocked.status == http.StatusOK {
		t.Fatalf("code must be consumed after repeated failures, got %d", blocked.status)
	}
}

// Қолдау көрсетілмейтін нөмір тіркелмейді.
func TestPhoneValidation(t *testing.T) {
	h := newHarness(t)
	for _, identifier := range []string{"+1 202 555 0143", "12345", "+7 111 123 45 67"} {
		res := h.do(http.MethodPost, "/api/v1/auth/request-otp", map[string]any{"identifier": identifier}, nil)
		if res.status != http.StatusBadRequest {
			t.Fatalf("identifier %q accepted (status %d)", identifier, res.status)
		}
	}
	ok := h.do(http.MethodPost, "/api/v1/auth/request-otp", map[string]any{"identifier": "+998 90 123 45 67"}, nil)
	if ok.status != http.StatusOK {
		t.Fatalf("uzbek number rejected: %d %s", ok.status, ok.raw)
	}
}

// Access токеннің мерзімі өткенде refresh ротациямен жаңартылады.
func TestAccessTokenExpiryAndRefreshRotation(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 701 222 33 44")

	h.clock.Advance(16 * time.Minute)
	expired := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(session.access))
	if expired.status != http.StatusUnauthorized {
		t.Fatalf("expired access token accepted: %d", expired.status)
	}

	refreshed := h.do(http.MethodPost, "/api/v1/auth/refresh",
		map[string]any{"refresh_token": session.refresh}, nil)
	if refreshed.status != http.StatusOK {
		t.Fatalf("refresh failed: %d %s", refreshed.status, refreshed.raw)
	}
	newAccess := refreshed.str("access_token")
	newRefresh := refreshed.str("refresh_token")
	if newRefresh == session.refresh {
		t.Fatal("refresh token must rotate")
	}
	if ok := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(newAccess)); ok.status != http.StatusOK {
		t.Fatalf("new access token rejected: %d", ok.status)
	}

	// Ескі токенді қайта пайдалану — бүкіл тізбек жабылады.
	reused := h.do(http.MethodPost, "/api/v1/auth/refresh",
		map[string]any{"refresh_token": session.refresh}, nil)
	if reused.status != http.StatusUnauthorized {
		t.Fatalf("reused refresh token accepted: %d", reused.status)
	}
	afterReuse := h.do(http.MethodPost, "/api/v1/auth/refresh",
		map[string]any{"refresh_token": newRefresh}, nil)
	if afterReuse.status != http.StatusUnauthorized {
		t.Fatalf("family not revoked after reuse detection: %d", afterReuse.status)
	}
}

// Шығу сессияны жабады.
func TestLogoutRevokesRefreshToken(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 701 333 44 55")

	out := h.do(http.MethodPost, "/api/v1/auth/logout",
		map[string]any{"refresh_token": session.refresh}, h.auth(session.access))
	if out.status != http.StatusOK {
		t.Fatalf("logout failed: %d", out.status)
	}
	after := h.do(http.MethodPost, "/api/v1/auth/refresh",
		map[string]any{"refresh_token": session.refresh}, nil)
	if after.status != http.StatusUnauthorized {
		t.Fatalf("revoked refresh accepted: %d", after.status)
	}
}

// Өшірілген есептік жазба қызметке жіберілмейді.
func TestDisabledAccountIsRejected(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 701 444 55 66")
	adminSession := h.signInAdmin()

	res := h.do(http.MethodPost, "/api/v1/admin/users/"+session.userID+"/status",
		map[string]any{"status": "disabled"}, adminSession.headers(h.cfg.Admin.CookieName))
	if res.status != http.StatusOK {
		t.Fatalf("disable failed: %d %s", res.status, res.raw)
	}

	blocked := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(session.access))
	if blocked.status != http.StatusForbidden || blocked.errorCode() != "ACCOUNT_DISABLED" {
		t.Fatalf("got %d %s, want 403 ACCOUNT_DISABLED", blocked.status, blocked.errorCode())
	}
}

// Параллель кіру де бір ғана есептік жазба жасайды.
func TestConcurrentSignInCreatesOneAccount(t *testing.T) {
	h := newHarness(t)
	identifier := "+7 705 999 88 77"
	h.do(http.MethodPost, "/api/v1/auth/request-otp", map[string]any{"identifier": identifier}, nil)

	var wg sync.WaitGroup
	results := make([]response, 4)
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index] = h.do(http.MethodPost, "/api/v1/auth/verify-otp",
				map[string]any{"identifier": identifier, "code": "1111"}, nil)
		}(i)
	}
	wg.Wait()

	success := 0
	for _, res := range results {
		if res.status == http.StatusOK {
			success++
		}
	}
	if success == 0 {
		t.Fatal("no successful sign-in")
	}
	if success > 1 {
		t.Fatalf("otp consumed more than once: %d successes", success)
	}
}

// Профиль PATCH арқылы да, POST арқылы да жаңарады (Android клиенті үшін).
func TestProfileUpdateAcceptsPostAlias(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 707 555 44 33")

	patched := h.do(http.MethodPatch, "/api/v1/me",
		map[string]any{"role": "сатушы", "preferred_tone": "professional"}, h.auth(session.access))
	if patched.status != http.StatusOK {
		t.Fatalf("PATCH /me: %d %s", patched.status, patched.raw)
	}

	posted := h.do(http.MethodPost, "/api/v1/me",
		map[string]any{"description": "Парфюм сатамын", "onboarding_completed": true}, h.auth(session.access))
	if posted.status != http.StatusOK {
		t.Fatalf("POST /me: %d %s", posted.status, posted.raw)
	}

	me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(session.access))
	if role := me.str("profile", "role"); role != "сатушы" {
		t.Fatalf("role = %q after update", role)
	}
	if done := me.body["profile"].(map[string]any)["onboarding_completed"]; done != true {
		t.Fatalf("onboarding flag not stored: %v", done)
	}
}

// Профиль жазылмаса — тіркелгі де жоқ (жартылай тіркелгі қалмайды).
func TestAFailedProfileWriteLeavesNoHalfAccount(t *testing.T) {
	h := newHarness(t)
	h.exec(`CREATE TRIGGER fail_profile BEFORE INSERT ON user_profiles BEGIN SELECT RAISE(ABORT, 'disk full'); END`)
	mustStatus(t, h.do(http.MethodPost, "/api/v1/auth/request-otp",
		map[string]any{"identifier": "+7 701 444 00 11", "locale": "kk"}, nil), http.StatusOK, "")
	verify := h.do(http.MethodPost, "/api/v1/auth/verify-otp", map[string]any{
		"identifier": "+7 701 444 00 11", "code": "1111", "device": map[string]any{"platform": "android"},
	}, nil)
	if verify.status != http.StatusInternalServerError {
		t.Fatalf("sign-in with a failing profile write: %d %s", verify.status, verify.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM users WHERE phone = '+77014440011'`); n != 0 {
		t.Fatalf("%d account(s) left without a profile", n)
	}
	h.exec(`DROP TRIGGER fail_profile`)
	if s := h.signIn("+7 701 444 00 11"); !s.isNew {
		t.Fatal("the retried sign-in did not create the account")
	}
}
