package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/traits"
)

type handoffNativeSession struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	User    struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
}

func handoffNativeSignIn(t *testing.T, app *application, email string) handoffNativeSession {
	t.Helper()
	origin := "https://billing.example.invalid"
	w := webTestRequest(app.handler, http.MethodPost, "/api/v1/auth/email/otp/request", map[string]string{"email": email, "locale": "ru"}, nil, origin, "")
	if w.Code != http.StatusOK {
		t.Fatal("native sign-in request failed")
	}
	w = webTestRequest(app.handler, http.MethodPost, "/api/v1/auth/email/otp/verify", map[string]any{"email": email, "code": "1111", "device": map[string]string{"platform": "ios", "device_id": traits.NewID(), "app_version": "1.2.0"}}, nil, origin, "")
	var session handoffNativeSession
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &session) != nil || session.Access == "" || session.User.ID == "" {
		t.Fatal("native sign-in verification failed")
	}
	return session
}

func handoffMobileRequest(t *testing.T, app *application, access, expected string) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"expected_user_id": expected, "locale": "ru"})
	r := httptest.NewRequest(http.MethodPost, "https://billing.example.invalid/api/v1/web/payment-handoff", strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", "application/json")
	if access != "" {
		r.Header.Set("Authorization", "Bearer "+access)
	}
	w := httptest.NewRecorder()
	app.handler.ServeHTTP(w, r)
	return w
}

func handoffCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var result struct {
		URL     string `json:"url"`
		Expires int    `json:"expires_in"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal("handoff creation failed")
	}
	u, err := url.Parse(result.URL)
	if err != nil || u.Scheme != "https" || u.Host != "billing.example.invalid" || u.Path != "/payment" || u.RawQuery != "" || !strings.HasPrefix(u.Fragment, "handoff=") || result.Expires != 60 {
		t.Fatal("handoff URL is not a bounded website capability")
	}
	code := strings.TrimPrefix(u.Fragment, "handoff=")
	if len(code) != 43 {
		t.Fatal("handoff has wrong entropy representation")
	}
	return code
}

func handoffWebCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "aireply_web_session" {
			if !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/account/" || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatal("unsafe handoff cookie")
			}
			return cookie
		}
	}
	t.Fatal("handoff did not set a browser cookie")
	return nil
}

func TestWebHandoffAuthenticatesSameAccountWithoutExposingNativeCredentials(t *testing.T) {
	cfg := webTestConfig(t)
	app := webTestApplication(t, cfg)
	native := handoffNativeSignIn(t, app, "handoff-web@example.invalid")
	code := handoffCode(t, handoffMobileRequest(t, app, native.Access, native.User.ID))
	body := map[string]any{"code": code, "confirm_account_switch": false}
	w := webTestRequest(app.handler, http.MethodPost, "/account/api/handoff", body, nil, "https://billing.example.invalid", "")
	if w.Code != http.StatusOK {
		t.Fatal("anonymous browser exchange failed")
	}
	cookie := handoffWebCookie(t, w)
	var snapshot webTestSnapshot
	if json.Unmarshal(w.Body.Bytes(), &snapshot) != nil || snapshot.User.ID != native.User.ID {
		t.Fatal("handoff changed recipient")
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(w.Body.Bytes(), &payload) != nil || len(payload) != 4 || payload["user"] == nil || payload["usage"] == nil || payload["subscription"] == nil || payload["bank"] == nil {
		t.Fatal("website snapshot exposed unrelated profile/consent data")
	}
	if strings.Contains(w.Body.String(), native.Access) || strings.Contains(w.Body.String(), native.Refresh) || strings.Contains(w.Body.String(), code) || cookie.Value == native.Refresh {
		t.Fatal("website exposed or reused native credentials")
	}
	w = webTestRequest(app.handler, http.MethodPost, "/account/api/handoff", body, cookie, "https://billing.example.invalid", "")
	if w.Code != http.StatusUnauthorized || len(w.Result().Cookies()) != 0 || !strings.Contains(w.Body.String(), "WEB_HANDOFF_INVALID") {
		t.Fatal("replay changed browser session")
	}
	w = webTestRequest(app.handler, http.MethodPost, "/account/api/logout", map[string]any{}, cookie, "https://billing.example.invalid", "")
	if w.Code != http.StatusOK {
		t.Fatal("web logout failed")
	}
	w = webTestRequest(app.handler, http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refresh_token": native.Refresh, "device": map[string]string{"platform": "ios"}}, nil, "https://billing.example.invalid", "")
	if w.Code != http.StatusOK {
		t.Fatal("web logout revoked native login")
	}
}

func TestWebHandoffConfirmsAccountSwitchAndKeepsCodeUntilConfirmation(t *testing.T) {
	app := webTestApplication(t, webTestConfig(t))
	native := handoffNativeSignIn(t, app, "handoff-incoming@example.invalid")
	old, current := webTestSignIn(t, app.handler, "", "handoff-current@example.invalid")
	code := handoffCode(t, handoffMobileRequest(t, app, native.Access, native.User.ID))
	body := map[string]any{"code": code, "confirm_account_switch": false}
	w := webTestRequest(app.handler, http.MethodPost, "/account/api/handoff", body, old, "https://billing.example.invalid", "")
	var failure struct {
		Error struct {
			Code    string            `json:"code"`
			Details map[string]string `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &failure)
	if w.Code != http.StatusConflict || failure.Error.Code != "WEB_ACCOUNT_SWITCH_REQUIRED" || failure.Error.Details["email"] != native.User.Email || failure.Error.Details["current_email"] != current.User.Email || len(w.Result().Cookies()) != 0 {
		t.Fatal("different browser account switched without confirmation")
	}
	unchanged := webTestRequest(app.handler, http.MethodGet, "/account/api/session", nil, old, "", "")
	var unchangedSnapshot webTestSnapshot
	if unchanged.Code != http.StatusOK || json.Unmarshal(unchanged.Body.Bytes(), &unchangedSnapshot) != nil || unchangedSnapshot.User.ID != current.User.ID {
		t.Fatal("unconfirmed handoff ended the existing browser session")
	}
	body["confirm_account_switch"] = true
	w = webTestRequest(app.handler, http.MethodPost, "/account/api/handoff", body, old, "https://billing.example.invalid", "")
	if w.Code != http.StatusOK {
		t.Fatal("confirmation could not consume the original handoff")
	}
	next := handoffWebCookie(t, w)
	var snapshot webTestSnapshot
	_ = json.Unmarshal(w.Body.Bytes(), &snapshot)
	if snapshot.User.ID != native.User.ID {
		t.Fatal("confirmed switch used the wrong recipient")
	}
	if oldState := webTestRequest(app.handler, http.MethodGet, "/account/api/session", nil, old, "", ""); oldState.Code != http.StatusUnauthorized {
		t.Fatal("old browser session survived switch")
	}
	// Same-account exchange does not need a second confirmation.
	code = handoffCode(t, handoffMobileRequest(t, app, native.Access, native.User.ID))
	w = webTestRequest(app.handler, http.MethodPost, "/account/api/handoff", map[string]any{"code": code}, next, "https://billing.example.invalid", "")
	if w.Code != http.StatusOK {
		t.Fatal("same-account browser required a switch confirmation")
	}
}

func TestWebHandoffBindsRecipientOriginAndCreationRate(t *testing.T) {
	app := webTestApplication(t, webTestConfig(t))
	native := handoffNativeSignIn(t, app, "handoff-binding@example.invalid")
	if w := handoffMobileRequest(t, app, "", native.User.ID); w.Code != http.StatusUnauthorized {
		t.Fatal("anonymous app created a handoff")
	}
	if w := handoffMobileRequest(t, app, native.Access, "wrong-account"); w.Code != http.StatusConflict {
		t.Fatal("account-switch race created a handoff")
	}
	code := handoffCode(t, handoffMobileRequest(t, app, native.Access, native.User.ID))
	w := webTestRequest(app.handler, http.MethodPost, "/account/api/handoff", map[string]any{"code": code}, nil, "https://other.example", "")
	if w.Code != http.StatusForbidden {
		t.Fatal("cross-origin handoff exchanged")
	}
	// Invalid origin must leave the one-use code available to the real page.
	w = webTestRequest(app.handler, http.MethodPost, "/account/api/handoff", map[string]any{"code": code}, nil, "https://billing.example.invalid", "")
	if w.Code != http.StatusOK {
		t.Fatal("invalid origin consumed code")
	}
	for i := 0; i < 3; i++ {
		if w := handoffMobileRequest(t, app, native.Access, native.User.ID); w.Code != http.StatusOK {
			t.Fatal("valid create limit reached too early")
		}
	}
	if w := handoffMobileRequest(t, app, native.Access, native.User.ID); w.Code != http.StatusTooManyRequests {
		t.Fatal("handoff creation was not limited per user")
	}
}
