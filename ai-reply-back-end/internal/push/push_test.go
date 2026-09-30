package push

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------- keys

func rsaPEM(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func ecPEM(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func decodeJWT(t *testing.T, token string) (header, claims map[string]any, signing string, sig []byte) {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d parts", len(parts))
	}
	raw := func(s string) []byte {
		b, err := base64.RawURLEncoding.DecodeString(s)
		if err != nil {
			t.Fatalf("base64: %v", err)
		}
		return b
	}
	_ = json.Unmarshal(raw(parts[0]), &header)
	_ = json.Unmarshal(raw(parts[1]), &claims)
	return header, claims, parts[0] + "." + parts[1], raw(parts[2])
}

func testMessage() Message {
	return Message{
		Title: "Тариф скоро закончится", Body: "Продлите тариф, чтобы сохранить лимит.",
		Data:       map[string]string{"nid": "n-1", "did": "d-1", "type": "subscription_expiring", "link": "aireply://subscription"},
		Category:   "subscription",
		CollapseID: "n-1",
		TTL:        24 * time.Hour,
		Important:  true,
	}
}

// ---------------------------------------------------------------- FCM

type fakeGoogle struct {
	t           *testing.T
	pub         *rsa.PublicKey
	tokenCalls  atomic.Int32
	sendCalls   atomic.Int32
	lastMessage map[string]any
	mu          sync.Mutex
	respond     func(w http.ResponseWriter)
	server      *httptest.Server
}

func newFakeGoogle(t *testing.T, pub *rsa.PublicKey) *fakeGoogle {
	g := &fakeGoogle{t: t, pub: pub}
	g.server = httptest.NewServer(http.HandlerFunc(g.handle))
	t.Cleanup(g.server.Close)
	return g
}

func (g *fakeGoogle) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/token":
		g.tokenCalls.Add(1)
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
			return
		}
		header, claims, signing, sig := decodeJWT(g.t, r.Form.Get("assertion"))
		digest := sha256.Sum256([]byte(signing))
		if header["alg"] != "RS256" || rsa.VerifyPKCS1v15(g.pub, crypto.SHA256, digest[:], sig) != nil {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		if claims["iss"] != "push@ai-reply.iam.gserviceaccount.com" ||
			claims["scope"] != "https://www.googleapis.com/auth/firebase.messaging" ||
			claims["aud"] != g.server.URL+"/token" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"ya29.test-access","expires_in":3599,"token_type":"Bearer"}`)
	case r.URL.Path == "/v1/projects/ai-reply/messages:send":
		g.sendCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer ya29.test-access" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		g.mu.Lock()
		g.lastMessage = body
		respond := g.respond
		g.mu.Unlock()
		if respond != nil {
			respond(w)
			return
		}
		_, _ = io.WriteString(w, `{"name":"projects/ai-reply/messages/0:1700000000000000%abc"}`)
	default:
		http.NotFound(w, r)
	}
}

func newTestFCM(t *testing.T, g *fakeGoogle, key string, now func() time.Time) *FCM {
	t.Helper()
	provider, err := NewFCM(FCMConfig{
		ProjectID: "ai-reply", ClientEmail: "push@ai-reply.iam.gserviceaccount.com", PrivateKey: key,
		TokenURL: g.server.URL + "/token", Endpoint: g.server.URL, HTTPClient: g.server.Client(), Now: now,
	})
	if err != nil {
		t.Fatalf("NewFCM: %v", err)
	}
	return provider
}

func TestFCMSendsHTTPv1MessageWithServiceAccountToken(t *testing.T) {
	key, pemKey := rsaPEM(t)
	g := newFakeGoogle(t, &key.PublicKey)
	provider := newTestFCM(t, g, pemKey, nil)

	res := provider.Send(context.Background(), Target{Token: "fcm-token-1"}, testMessage())
	if res.Outcome != Accepted || !strings.HasPrefix(res.MessageID, "projects/ai-reply/messages/") {
		t.Fatalf("result = %+v", res)
	}
	message := g.lastMessage["message"].(map[string]any)
	if message["token"] != "fcm-token-1" {
		t.Fatalf("token = %v", message["token"])
	}
	notification := message["notification"].(map[string]any)
	if notification["title"] != "Тариф скоро закончится" {
		t.Fatalf("notification = %v", notification)
	}
	data := message["data"].(map[string]any)
	if data["nid"] != "n-1" || data["link"] != "aireply://subscription" {
		t.Fatalf("data = %v", data)
	}
	android := message["android"].(map[string]any)
	androidNotification := android["notification"].(map[string]any)
	if android["priority"] != "HIGH" || android["ttl"] != "86400s" ||
		androidNotification["channel_id"] != "important" || androidNotification["tag"] != "n-1" {
		t.Fatalf("android = %v", android)
	}

	// The OAuth token is cached: a second send does not ask Google again.
	_ = provider.Send(context.Background(), Target{Token: "fcm-token-2"}, testMessage())
	if g.tokenCalls.Load() != 1 || g.sendCalls.Load() != 2 {
		t.Fatalf("token calls %d, sends %d", g.tokenCalls.Load(), g.sendCalls.Load())
	}
}

func TestFCMRefreshesTheAccessTokenBeforeItExpires(t *testing.T) {
	key, pemKey := rsaPEM(t)
	g := newFakeGoogle(t, &key.PublicKey)
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	provider := newTestFCM(t, g, pemKey, func() time.Time { return now })

	_ = provider.Send(context.Background(), Target{Token: "t"}, testMessage())
	now = now.Add(56 * time.Minute) // < 5 minutes left of 3599 s
	_ = provider.Send(context.Background(), Target{Token: "t"}, testMessage())
	if g.tokenCalls.Load() != 2 {
		t.Fatalf("token calls = %d, want a refresh", g.tokenCalls.Load())
	}
}

func TestFCMClassification(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name    string
		status  int
		body    string
		retry   string
		outcome Outcome
		code    string
	}{
		{"unregistered", 404, `{"error":{"code":404,"status":"NOT_FOUND","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"UNREGISTERED"}]}}`, "", InvalidToken, "UNREGISTERED"},
		{"sender mismatch", 403, `{"error":{"code":403,"status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"SENDER_ID_MISMATCH"}]}}`, "", InvalidToken, "SENDER_ID_MISMATCH"},
		{"bad token format", 400, `{"error":{"code":400,"status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"INVALID_ARGUMENT"},{"@type":"type.googleapis.com/google.rpc.BadRequest","fieldViolations":[{"field":"message.token","description":"The registration token is not a valid FCM registration token"}]}]}}`, "", InvalidToken, "INVALID_ARGUMENT"},
		{"bad payload", 400, `{"error":{"code":400,"status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"INVALID_ARGUMENT"},{"@type":"type.googleapis.com/google.rpc.BadRequest","fieldViolations":[{"field":"message.android.ttl"}]}]}}`, "", Rejected, "INVALID_ARGUMENT"},
		{"quota", 429, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"QUOTA_EXCEEDED"}]}}`, "30", Retry, "QUOTA_EXCEEDED"},
		{"unavailable", 503, `{"error":{"code":503,"status":"UNAVAILABLE"}}`, "", Retry, "UNAVAILABLE"},
		{"internal", 500, `{"error":{"code":500,"status":"INTERNAL"}}`, "", Retry, "INTERNAL"},
		{"html 502", 502, `<html>bad gateway</html>`, "", Retry, "HTTP_502"},
		{"project not found", 404, `{"error":{"code":404,"status":"NOT_FOUND"}}`, "", Rejected, "NOT_FOUND"},
	}
	for _, tc := range cases {
		res := ClassifyFCM(tc.status, []byte(tc.body), tc.retry, now)
		if res.Outcome != tc.outcome || res.Code != tc.code {
			t.Errorf("%s: got %v %q, want %v %q", tc.name, res.Outcome, res.Code, tc.outcome, tc.code)
		}
	}
	if res := ClassifyFCM(429, nil, "30", now); res.RetryAfter != 30*time.Second {
		t.Errorf("Retry-After ignored: %v", res.RetryAfter)
	}
}

func TestFCMRefetchesTheOAuthTokenAfter401(t *testing.T) {
	key, pemKey := rsaPEM(t)
	g := newFakeGoogle(t, &key.PublicKey)
	provider := newTestFCM(t, g, pemKey, nil)
	var calls atomic.Int32
	g.respond = func(w http.ResponseWriter) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"name":"projects/ai-reply/messages/2"}`)
	}
	if res := provider.Send(context.Background(), Target{Token: "t"}, testMessage()); res.Outcome != Retry {
		t.Fatalf("401 must be retried: %+v", res)
	}
	if res := provider.Send(context.Background(), Target{Token: "t"}, testMessage()); res.Outcome != Accepted {
		t.Fatalf("second send: %+v", res)
	}
	if g.tokenCalls.Load() != 2 {
		t.Fatalf("token calls = %d, want a fresh token after 401", g.tokenCalls.Load())
	}
}

func TestFCMTimeoutIsRetryable(t *testing.T) {
	key, pemKey := rsaPEM(t)
	g := newFakeGoogle(t, &key.PublicKey)
	provider := newTestFCM(t, g, pemKey, nil)
	release := make(chan struct{})
	defer close(release)
	g.respond = func(w http.ResponseWriter) { <-release }

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	res := provider.Send(ctx, Target{Token: "t"}, testMessage())
	if res.Outcome != Retry || res.Code != "TIMEOUT" {
		t.Fatalf("result = %+v", res)
	}
}

func TestNewFCMRejectsBadKeysWithoutEchoingThem(t *testing.T) {
	_, err := NewFCM(FCMConfig{ProjectID: "p", ClientEmail: "a@b", PrivateKey: "-----BEGIN PRIVATE KEY-----\nc2VjcmV0\n-----END PRIVATE KEY-----\n"})
	if err == nil || strings.Contains(err.Error(), "c2VjcmV0") {
		t.Fatalf("err = %v", err)
	}
	_, ecKey := ecPEM(t)
	if _, err := NewFCM(FCMConfig{ProjectID: "p", ClientEmail: "a@b", PrivateKey: ecKey}); err == nil {
		t.Fatal("an EC key is not a service-account key")
	}
}

// ---------------------------------------------------------------- APNs

type fakeApple struct {
	t        *testing.T
	pub      *ecdsa.PublicKey
	server   *httptest.Server
	mu       sync.Mutex
	requests []*http.Request
	bodies   []map[string]any
	jwts     []string
	respond  func(w http.ResponseWriter, r *http.Request)
}

func newFakeApple(t *testing.T, pub *ecdsa.PublicKey) *fakeApple {
	a := &fakeApple{t: t, pub: pub}
	a.server = httptest.NewUnstartedServer(http.HandlerFunc(a.handle))
	a.server.EnableHTTP2 = true
	a.server.StartTLS()
	t.Cleanup(a.server.Close)
	return a
}

func (a *fakeApple) handle(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	jwt := strings.TrimPrefix(r.Header.Get("authorization"), "bearer ")
	a.mu.Lock()
	a.requests = append(a.requests, r)
	a.bodies = append(a.bodies, body)
	a.jwts = append(a.jwts, jwt)
	respond := a.respond
	a.mu.Unlock()

	if r.ProtoMajor != 2 {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"reason":"BadPath"}`)
		return
	}
	header, claims, signing, sig := decodeJWT(a.t, jwt)
	digest := sha256.Sum256([]byte(signing))
	valid := len(sig) == 64 && ecdsa.Verify(a.pub, digest[:],
		new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:]))
	if !valid || header["alg"] != "ES256" || header["kid"] != "ABC123DEFG" || claims["iss"] != "JXM8N66QWU" {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"reason":"InvalidProviderToken"}`)
		return
	}
	if respond != nil {
		respond(w, r)
		return
	}
	w.Header().Set("apns-id", "5E2F1C3A-0000-4000-8000-000000000001")
	w.WriteHeader(http.StatusOK)
}

func newTestAPNs(t *testing.T, a *fakeApple, key string, env string, now func() time.Time) *APNs {
	t.Helper()
	provider, err := NewAPNs(APNsConfig{
		KeyID: "ABC123DEFG", TeamID: "JXM8N66QWU", BundleID: "kz.yerek.replykeyboard", PrivateKey: key,
		DefaultEnvironment: env, ProductionURL: a.server.URL + "/prod", SandboxURL: a.server.URL + "/sandbox",
		HTTPClient: a.server.Client(), Now: now,
	})
	if err != nil {
		t.Fatalf("NewAPNs: %v", err)
	}
	return provider
}

func TestAPNsSendsOverHTTP2WithTokenAuth(t *testing.T) {
	key, pemKey := ecPEM(t)
	apple := newFakeApple(t, &key.PublicKey)
	provider := newTestAPNs(t, apple, pemKey, "production", nil)

	token := strings.Repeat("ab", 32)
	res := provider.Send(context.Background(), Target{Token: token, Environment: "production"}, testMessage())
	if res.Outcome != Accepted || res.MessageID == "" {
		t.Fatalf("result = %+v", res)
	}
	req := apple.requests[0]
	if req.URL.Path != "/prod/3/device/"+token {
		t.Fatalf("path = %s", req.URL.Path)
	}
	for header, want := range map[string]string{
		"apns-topic": "kz.yerek.replykeyboard", "apns-push-type": "alert",
		"apns-priority": "10", "apns-collapse-id": "n-1",
	} {
		if got := req.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if req.Header.Get("apns-expiration") == "" {
		t.Error("apns-expiration missing")
	}
	body := apple.bodies[0]
	aps := body["aps"].(map[string]any)
	alert := aps["alert"].(map[string]any)
	if alert["title"] != "Тариф скоро закончится" || aps["sound"] != "default" || aps["thread-id"] != "subscription" {
		t.Fatalf("aps = %v", aps)
	}
	if body["nid"] != "n-1" || body["link"] != "aireply://subscription" {
		t.Fatalf("custom keys = %v", body)
	}
}

func TestAPNsReusesTheProviderTokenAndRenewsIt(t *testing.T) {
	key, pemKey := ecPEM(t)
	apple := newFakeApple(t, &key.PublicKey)
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	provider := newTestAPNs(t, apple, pemKey, "production", func() time.Time { return now })

	target := Target{Token: strings.Repeat("cd", 32), Environment: "production"}
	provider.Send(context.Background(), target, testMessage())
	provider.Send(context.Background(), target, testMessage())
	now = now.Add(41 * time.Minute)
	provider.Send(context.Background(), target, testMessage())
	if apple.jwts[0] != apple.jwts[1] {
		t.Fatal("the provider token must be reused within its lifetime")
	}
	if apple.jwts[1] == apple.jwts[2] {
		t.Fatal("the provider token must be renewed after 40 minutes")
	}
}

func TestAPNsClassification(t *testing.T) {
	now := time.Now()
	cases := []struct {
		status  int
		reason  string
		outcome Outcome
	}{
		{400, "BadDeviceToken", InvalidToken},
		{400, "DeviceTokenNotForTopic", InvalidToken},
		{410, "Unregistered", InvalidToken},
		{410, "ExpiredToken", InvalidToken},
		{429, "TooManyRequests", Retry},
		{500, "InternalServerError", Retry},
		{503, "ServiceUnavailable", Retry},
		{403, "ExpiredProviderToken", Retry},
		{400, "BadTopic", Rejected},
		{400, "PayloadEmpty", Rejected},
		{413, "PayloadTooLarge", Rejected},
		{403, "InvalidProviderToken", Rejected},
	}
	for _, tc := range cases {
		res := ClassifyAPNs(tc.status, []byte(`{"reason":"`+tc.reason+`"}`), "", now)
		if res.Outcome != tc.outcome || res.Code != tc.reason {
			t.Errorf("%d %s: got %v %q", tc.status, tc.reason, res.Outcome, res.Code)
		}
	}
}

func TestAPNsTriesTheOtherEnvironmentOnlyWhenItGuessed(t *testing.T) {
	key, pemKey := ecPEM(t)
	apple := newFakeApple(t, &key.PublicKey)
	apple.respond = func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/prod/") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"reason":"BadDeviceToken"}`)
			return
		}
		w.Header().Set("apns-id", "sandbox-id")
		w.WriteHeader(http.StatusOK)
	}
	provider := newTestAPNs(t, apple, pemKey, "production", nil)
	token := strings.Repeat("ef", 32)

	if res := provider.Send(context.Background(), Target{Token: token}, testMessage()); res.Outcome != Accepted || res.MessageID != "sandbox-id" {
		t.Fatalf("guessed environment: %+v", res)
	}
	// The app said "production": its token is dead there, no second guess.
	if res := provider.Send(context.Background(), Target{Token: token, Environment: "production"}, testMessage()); res.Outcome != InvalidToken {
		t.Fatalf("explicit environment: %+v", res)
	}
}

func TestAPNsExpiredProviderTokenIsRenewed(t *testing.T) {
	key, pemKey := ecPEM(t)
	apple := newFakeApple(t, &key.PublicKey)
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	provider := newTestAPNs(t, apple, pemKey, "production", func() time.Time { return now })
	var calls atomic.Int32
	apple.respond = func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 2 {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"reason":"ExpiredProviderToken"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
	target := Target{Token: strings.Repeat("aa", 32), Environment: "production"}
	provider.Send(context.Background(), target, testMessage())
	now = now.Add(2 * time.Minute)
	if res := provider.Send(context.Background(), target, testMessage()); res.Outcome != Retry {
		t.Fatalf("expired provider token: %+v", res)
	}
	provider.Send(context.Background(), target, testMessage())
	if apple.jwts[1] == apple.jwts[2] {
		t.Fatal("an expired provider token must be replaced")
	}
}

func TestAPNsNetworkErrorsNeverLeakTheDeviceToken(t *testing.T) {
	_, pemKey := ecPEM(t)
	provider, err := NewAPNs(APNsConfig{
		KeyID: "ABC123DEFG", TeamID: "JXM8N66QWU", BundleID: "kz.yerek.replykeyboard", PrivateKey: pemKey,
		ProductionURL: "http://127.0.0.1:1", HTTPClient: &http.Client{Timeout: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("99", 32)
	res := provider.Send(context.Background(), Target{Token: token, Environment: "production"}, testMessage())
	if res.Outcome != Retry || strings.Contains(res.Detail, token) {
		t.Fatalf("result = %+v", res)
	}
}

func TestNewAPNsValidatesIdentifiersAndKey(t *testing.T) {
	_, pemKey := ecPEM(t)
	_, rsaKey := rsaPEM(t)
	for name, cfg := range map[string]APNsConfig{
		"key id":  {KeyID: "short", TeamID: "JXM8N66QWU", BundleID: "b", PrivateKey: pemKey},
		"team id": {KeyID: "ABC123DEFG", TeamID: "x", BundleID: "b", PrivateKey: pemKey},
		"bundle":  {KeyID: "ABC123DEFG", TeamID: "JXM8N66QWU", PrivateKey: pemKey},
		"rsa key": {KeyID: "ABC123DEFG", TeamID: "JXM8N66QWU", BundleID: "b", PrivateKey: rsaKey},
	} {
		if _, err := NewAPNs(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPayloadSizesStayUnderProviderLimits(t *testing.T) {
	msg := testMessage()
	msg.Body = strings.Repeat("ә", 400)
	fcm, apns := PayloadSizes(msg)
	if fcm > MaxFCMPayloadBytes || apns > MaxAPNsPayloadBytes {
		t.Fatalf("sizes fcm=%d apns=%d", fcm, apns)
	}
}

func TestTokenHashIsStableHex(t *testing.T) {
	if TokenHash("abc") != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal("sha256 mismatch")
	}
}
