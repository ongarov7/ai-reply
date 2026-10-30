package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/legal"
	"github.com/aireply/ai-reply-back-end/internal/payments/bcc"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

type webTestSnapshot struct {
	User struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
	Usage struct {
		Purchased int `json:"purchased_credits"`
		Remaining int `json:"remaining_total"`
	} `json:"usage"`
	Bank struct {
		Enabled     bool              `json:"enabled"`
		Environment string            `json:"environment"`
		Version     int64             `json:"catalog_version"`
		Terms       string            `json:"terms_version"`
		Products    []catalog.Product `json:"products"`
	} `json:"bank"`
}

func webTestApplication(t *testing.T, cfg config.Config) *application {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	app, err := buildApplication(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), &workers)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); workers.Wait(); app.close() })
	return app
}

func webTestConfig(t *testing.T) config.Config {
	cfg := billingPrimaryFixture(t)
	cfg.BillingSandboxEnabled = false
	cfg.Auth.OTPResendCooldown = time.Nanosecond
	cfg.BCCCheckout = bcc.CheckoutConfig{
		Enabled: true, Environment: bcc.Sandbox, AppEnv: "test", PublicBaseURL: cfg.App.PublicBaseURL,
		Merchant: "00000001", MerchantName: "TEST MERCHANT", Terminal: "88888881",
		MACKeyHex: "6BB0AC02E47BDF73D98FEB777F3B5294", NotifyUsername: "fixture-bank",
		NotifyPassword: strings.Repeat("n", 40), StateSecret: strings.Repeat("s", 40),
	}
	return cfg
}

func webTestRequest(handler http.Handler, method, path string, body any, cookie *http.Cookie, origin string, key string) *httptest.ResponseRecorder {
	var raw io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		raw = strings.NewReader(string(encoded))
	}
	r := httptest.NewRequest(method, "https://billing.example.invalid"+path, raw)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Requested-With", "AIReplyAccount")
		r.Header.Set("Origin", origin)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func webTestSignIn(t *testing.T, handler http.Handler, prefix, email string) (*http.Cookie, webTestSnapshot) {
	t.Helper()
	origin := "https://billing.example.invalid"
	request := webTestRequest(handler, http.MethodPost, prefix+"/account/api/email/request", map[string]string{"email": email, "locale": "ru"}, nil, origin, "")
	if request.Code != http.StatusOK {
		t.Fatalf("request failed: %d %s", request.Code, request.Body.String())
	}
	verify := webTestRequest(handler, http.MethodPost, prefix+"/account/api/email/verify", map[string]any{"email": email, "code": "1111", "device": map[string]string{"platform": "ios", "device_id": "untrusted-device"}}, nil, origin, "")
	if verify.Code != http.StatusOK {
		t.Fatalf("verify failed: %d %s", verify.Code, verify.Body.String())
	}
	if strings.Contains(verify.Body.String(), "access_token") || strings.Contains(verify.Body.String(), "refresh_token") {
		t.Fatal("browser JSON exposed authentication tokens")
	}
	var snapshot webTestSnapshot
	if err := json.Unmarshal(verify.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	for _, c := range verify.Result().Cookies() {
		if strings.HasPrefix(c.Name, "aireply_web_") {
			if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != prefix+"/account/" {
				t.Fatal("browser session cookie is not isolated securely")
			}
			return c, snapshot
		}
	}
	t.Fatal("missing browser session cookie")
	return nil, snapshot
}

func webTestQuote(snapshot webTestSnapshot) map[string]any {
	return map[string]any{"product_code": "pack30", "expected_user_id": snapshot.User.ID,
		"expected_catalog_version": snapshot.Bank.Version, "expected_price_minor": int64(10000), "expected_units": 30,
		"terms_version": snapshot.Bank.Terms, "accept_terms": true}
}

func TestWebAccountUsesExistingAccountAndRevokesOnlyBrowserSession(t *testing.T) {
	cfg := webTestConfig(t)
	// Default HTTPS ports are normalized by browsers in the Origin header.
	cfg.App.PublicBaseURL += ":443"
	app := webTestApplication(t, cfg)
	db := billingOpenFixtureDB(t, cfg.Database.Path)
	store := repository.New(db)
	user, err := store.CreateUser(context.Background(), domain.User{ID: "existing-native-user", Email: "same@example.invalid", Status: domain.UserActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.InsertCreditGrant(context.Background(), domain.CreditGrant{UserID: user.ID, Kind: domain.GrantPack, UnitsTotal: 7, UnitsRemaining: 7}, time.Now()); err != nil {
		t.Fatal(err)
	}
	request := webTestRequest(app.handler, http.MethodPost, "/api/v1/auth/email/otp/request", map[string]string{"email": user.Email, "locale": "ru"}, nil, "https://billing.example.invalid", "")
	if request.Code != http.StatusOK {
		t.Fatal("native email challenge failed")
	}
	verify := webTestRequest(app.handler, http.MethodPost, "/api/v1/auth/email/otp/verify", map[string]any{"email": user.Email, "code": "1111", "device": map[string]string{"platform": "ios", "device_id": "native-iphone"}}, nil, "https://billing.example.invalid", "")
	var native struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if verify.Code != http.StatusOK || json.Unmarshal(verify.Body.Bytes(), &native) != nil || native.RefreshToken == "" {
		t.Fatal("native session failed")
	}
	cookie, snapshot := webTestSignIn(t, app.handler, "", user.Email)
	if snapshot.User.ID != user.ID || snapshot.Usage.Purchased != 7 {
		t.Fatal("web sign-in did not use the existing account and purchased balance")
	}
	if !snapshot.Bank.Enabled || snapshot.Bank.Version == 0 || snapshot.Bank.Terms != legal.WebPurchaseTermsVersion || len(snapshot.Bank.Products) != 1 {
		t.Fatal("incorrect web offer snapshot")
	}
	forged := *cookie
	forged.Value = native.RefreshToken
	if w := webTestRequest(app.handler, http.MethodGet, "/account/api/session", nil, &forged, "", ""); w.Code != http.StatusUnauthorized {
		t.Fatal("native refresh token authenticated a web session")
	}
	refresh := webTestRequest(app.handler, http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refresh_token": native.RefreshToken, "device": map[string]string{"platform": "ios"}}, nil, "https://billing.example.invalid", "")
	if refresh.Code != http.StatusOK || json.Unmarshal(refresh.Body.Bytes(), &native) != nil {
		t.Fatal("native session refresh failed after web sign-in")
	}
	claims, err := auth.ParseJWT(cfg.Auth.AccessSecret, native.AccessToken, time.Now())
	if err != nil || claims.Platform != "ios" {
		t.Fatal("website login changed the native session's platform")
	}
	preferenceRequest := httptest.NewRequest(http.MethodPut, "https://billing.example.invalid/api/v1/me/notification-preferences", strings.NewReader(`{"preferences":{"marketing":true},"expected_user_id":"another-account"}`))
	preferenceRequest.Header.Set("Authorization", "Bearer "+native.AccessToken)
	preferenceRequest.Header.Set("Content-Type", "application/json")
	preferenceResponse := httptest.NewRecorder()
	app.handler.ServeHTTP(preferenceResponse, preferenceRequest)
	if preferenceResponse.Code != http.StatusConflict {
		t.Fatal("marketing preference accepted an account that changed during authentication")
	}
	prefs, err := store.NotificationPreferences(context.Background(), user.ID)
	if err != nil || prefs[domain.CategoryMarketing] {
		t.Fatal("rejected preference request granted marketing consent")
	}
	for _, origin := range []string{"", "https://another.example.invalid"} {
		w := webTestRequest(app.handler, http.MethodPost, "/account/api/logout", map[string]any{}, cookie, origin, "")
		if w.Code != http.StatusForbidden {
			t.Fatal("cross-site request was accepted")
		}
	}
	if w := webTestRequest(app.handler, http.MethodGet, "/api/v1/me", nil, cookie, "", ""); w.Code != http.StatusUnauthorized {
		t.Fatal("browser cookie authenticated the native API")
	}
	w := webTestRequest(app.handler, http.MethodPost, "/account/api/logout", map[string]any{}, cookie, "https://billing.example.invalid", "")
	if w.Code != http.StatusOK {
		t.Fatal("browser logout failed")
	}
	if w = webTestRequest(app.handler, http.MethodGet, "/account/api/session", nil, cookie, "", ""); w.Code != http.StatusUnauthorized {
		t.Fatal("revoked browser session was accepted")
	}
	nativeRequest := httptest.NewRequest(http.MethodGet, "https://billing.example.invalid/api/v1/me", nil)
	nativeRequest.Header.Set("Authorization", "Bearer "+native.AccessToken)
	nativeResponse := httptest.NewRecorder()
	app.handler.ServeHTTP(nativeResponse, nativeRequest)
	if nativeResponse.Code != http.StatusOK {
		t.Fatal("browser logout invalidated the native app's session")
	}
}

func TestWebCheckoutBindsAccountAndTermsAndGrantsOnlyAfterBankNotify(t *testing.T) {
	cfg := webTestConfig(t)
	app := webTestApplication(t, cfg)
	cookie, first := webTestSignIn(t, app.handler, "", "first@example.invalid")
	other, second := webTestSignIn(t, app.handler, "", "second@example.invalid")
	body := webTestQuote(first)
	wrong := webTestRequest(app.handler, http.MethodPost, "/account/api/checkout", body, other, "https://billing.example.invalid", "cross-tab-attempt")
	if wrong.Code != http.StatusConflict || !strings.Contains(wrong.Body.String(), "WEB_ACCOUNT_CHANGED") {
		t.Fatal("checkout used another tab's account")
	}
	body["accept_terms"] = false
	if w := webTestRequest(app.handler, http.MethodPost, "/account/api/checkout", body, cookie, "https://billing.example.invalid", "missing-consent-attempt"); w.Code != http.StatusConflict {
		t.Fatal("checkout accepted missing purchase consent")
	}
	body["accept_terms"] = true
	body["expected_price_minor"] = int64(20000)
	if w := webTestRequest(app.handler, http.MethodPost, "/account/api/checkout", body, cookie, "https://billing.example.invalid", "stale-price-attempt"); w.Code != http.StatusConflict {
		t.Fatal("checkout accepted a changed price")
	}
	body["expected_price_minor"] = int64(10000)
	w := webTestRequest(app.handler, http.MethodPost, "/account/api/checkout", body, cookie, "https://billing.example.invalid", "verified-purchase-attempt")
	if w.Code != http.StatusCreated {
		t.Fatalf("checkout failed: %d %s", w.Code, w.Body.String())
	}
	var order bcc.BankOrder
	if err := json.Unmarshal(w.Body.Bytes(), &order); err != nil {
		t.Fatal(err)
	}
	if order.AmountMinor != 10000 || order.Units != 30 {
		t.Fatal("order did not use the confirmed quote")
	}
	if w = webTestRequest(app.handler, http.MethodGet, "/account/api/orders/"+order.ID, nil, other, "", ""); w.Code != http.StatusNotFound {
		t.Fatal("another user could read the order")
	}
	db := billingOpenFixtureDB(t, cfg.Database.Path)
	var consentCount int
	if err := db.Reader().QueryRow(`SELECT count(*) FROM billing_web_purchase_consents WHERE order_id=? AND user_id=? AND terms_version=?`, order.ID, first.User.ID, legal.WebPurchaseTermsVersion).Scan(&consentCount); err != nil || consentCount != 1 {
		t.Fatalf("purchase consent missing: %v", err)
	}
	prices := catalog.New(db)
	products := catalog.InitialProducts()
	products[5].PriceMinor = 20000
	draft, err := prices.SaveDraft(context.Background(), "operator", 0, products)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = prices.Publish(context.Background(), "operator", draft.ID, draft.Revision); err != nil {
		t.Fatal(err)
	}
	// A lost response is retried as the original order, even after publication.
	retry := webTestRequest(app.handler, http.MethodPost, "/account/api/checkout", body, cookie, "https://billing.example.invalid", "verified-purchase-attempt")
	var replay bcc.BankOrder
	_ = json.Unmarshal(retry.Body.Bytes(), &replay)
	if retry.Code != http.StatusCreated || replay.ID != order.ID || replay.AmountMinor != 10000 {
		t.Fatal("retry did not recover the original order")
	}
	var raw string
	if err = db.Reader().QueryRow(`SELECT request_json FROM billing_orders WHERE id=?`, order.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var fields url.Values
	if err = json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatal(err)
	}
	fields.Set("ACTION", "0")
	fields.Set("RC", "00")
	fields.Set("RRN", "123456789012")
	fields.Set("INT_REF", "ABCDEF0123456789")
	fields.Set("TRAN_TRTYPE", "1")
	before := webTestRequest(app.handler, http.MethodGet, "/account/api/session", nil, cookie, "", "")
	var current webTestSnapshot
	_ = json.Unmarshal(before.Body.Bytes(), &current)
	if current.Usage.Purchased != 0 {
		t.Fatal("pending order granted credits")
	}
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest(http.MethodPost, "https://billing.example.invalid/api/v1/payments/bcc/notify", strings.NewReader(fields.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.SetBasicAuth(cfg.BCCCheckout.NotifyUsername, cfg.BCCCheckout.NotifyPassword)
		response := httptest.NewRecorder()
		app.handler.ServeHTTP(response, r)
		if response.Code != http.StatusOK {
			t.Fatalf("final bank callback failed: %d %s", response.Code, response.Body.String())
		}
	}
	after := webTestRequest(app.handler, http.MethodGet, "/account/api/session", nil, cookie, "", "")
	_ = json.Unmarshal(after.Body.Bytes(), &current)
	if current.Usage.Purchased != 30 {
		t.Fatalf("verified purchase should grant exactly 30, got %d", current.Usage.Purchased)
	}
	otherBalance := webTestRequest(app.handler, http.MethodGet, "/account/api/session", nil, other, "", "")
	_ = json.Unmarshal(otherBalance.Body.Bytes(), &current)
	if current.User.ID != second.User.ID || current.Usage.Purchased != 0 {
		t.Fatal("purchase changed another account")
	}
}

func TestWebBrowserSessionsCannotCrossBillingSandbox(t *testing.T) {
	cfg := webTestConfig(t)
	primary := webTestApplication(t, cfg)
	child, err := billingSandboxConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Explicit local test fixture only, never provider credentials or live OTP.
	child.Auth.DemoMode = true
	child.Auth.DemoOTP = "1111"
	child.Auth.OTPChannel = "stub"
	sandbox := webTestApplication(t, child)
	primary.mux.Handle("/sandbox/", http.StripPrefix("/sandbox", sandbox.handler))
	mainCookie, _ := webTestSignIn(t, primary.handler, "", "same@example.invalid")
	sandboxCookie, _ := webTestSignIn(t, primary.handler, "/sandbox", "same@example.invalid")
	for _, tc := range []struct {
		prefix string
		cookie *http.Cookie
	}{{"/sandbox", mainCookie}, {"", sandboxCookie}} {
		w := webTestRequest(primary.handler, http.MethodGet, tc.prefix+"/account/api/session", nil, tc.cookie, "", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatal("browser session crossed environments")
		}
	}
}
