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
	"net/http"
	"net/http/httptest"
	"strconv"
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

// ---------------------------------------------------------------- fake Google

type fakeGoogle struct {
	t           *testing.T
	pub         *rsa.PublicKey
	tokenCalls  atomic.Int32
	sendCalls   atomic.Int32
	mu          sync.Mutex
	lastMessage map[string]any
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

func (g *fakeGoogle) setRespond(respond func(w http.ResponseWriter)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.respond = respond
}

func (g *fakeGoogle) message(t *testing.T) map[string]any {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	message, ok := g.lastMessage["message"].(map[string]any)
	if !ok {
		t.Fatalf("no message was sent: %v", g.lastMessage)
	}
	return message
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

// ---------------------------------------------------------------- tests

func TestFCMSendsHTTPv1MessageWithServiceAccountToken(t *testing.T) {
	key, pemKey := rsaPEM(t)
	g := newFakeGoogle(t, &key.PublicKey)
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	provider := newTestFCM(t, g, pemKey, func() time.Time { return now })

	res := provider.Send(context.Background(), Target{Token: "fcm-token-1"}, testMessage())
	if res.Outcome != Accepted || !strings.HasPrefix(res.MessageID, "projects/ai-reply/messages/") {
		t.Fatalf("result = %+v", res)
	}
	message := g.message(t)
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

	// iOS goes through the same request: the apns block makes it an alert
	// that collapses by notification id and expires with the TTL.
	apns := message["apns"].(map[string]any)
	headers := apns["headers"].(map[string]any)
	for name, want := range map[string]string{
		"apns-push-type": "alert", "apns-priority": "10", "apns-collapse-id": "n-1",
		"apns-expiration": strconv.FormatInt(now.Add(24*time.Hour).Unix(), 10),
	} {
		if headers[name] != want {
			t.Errorf("%s = %v, want %s", name, headers[name], want)
		}
	}
	aps := apns["payload"].(map[string]any)["aps"].(map[string]any)
	if aps["sound"] != "default" || aps["thread-id"] != "subscription" {
		t.Fatalf("aps = %v", aps)
	}

	// The OAuth token is cached: a second send does not ask Google again.
	_ = provider.Send(context.Background(), Target{Token: "fcm-token-2"}, testMessage())
	if g.tokenCalls.Load() != 1 || g.sendCalls.Load() != 2 {
		t.Fatalf("token calls %d, sends %d", g.tokenCalls.Load(), g.sendCalls.Load())
	}
}

func TestFCMOrdinaryCategoriesUseNormalPriority(t *testing.T) {
	key, pemKey := rsaPEM(t)
	g := newFakeGoogle(t, &key.PublicKey)
	provider := newTestFCM(t, g, pemKey, nil)
	msg := testMessage()
	msg.Important, msg.Category = false, "marketing"
	msg.CollapseID = strings.Repeat("ә", 40) // 80 bytes
	if res := provider.Send(context.Background(), Target{Token: "t"}, msg); res.Outcome != Accepted {
		t.Fatalf("result = %+v", res)
	}
	message := g.message(t)
	android := message["android"].(map[string]any)
	if android["priority"] != "NORMAL" || android["notification"].(map[string]any)["channel_id"] != "general" {
		t.Fatalf("android = %v", android)
	}
	headers := message["apns"].(map[string]any)["headers"].(map[string]any)
	collapse, _ := headers["apns-collapse-id"].(string)
	if headers["apns-priority"] != "5" || len(collapse) > 64 || collapse != strings.Repeat("ә", 32) {
		t.Fatalf("apns headers = %v", headers)
	}
}

func TestFCMRefreshesTheAccessTokenBeforeItExpires(t *testing.T) {
	key, pemKey := rsaPEM(t)
	g := newFakeGoogle(t, &key.PublicKey)
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	provider := newTestFCM(t, g, pemKey, func() time.Time { mu.Lock(); defer mu.Unlock(); return now })

	_ = provider.Send(context.Background(), Target{Token: "t"}, testMessage())
	mu.Lock()
	now = now.Add(56 * time.Minute) // < 5 minutes left of 3599 s
	mu.Unlock()
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
		{"apns key missing", 401, `{"error":{"code":401,"status":"UNAUTHENTICATED","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"THIRD_PARTY_AUTH_ERROR"}]}}`, "", Rejected, "THIRD_PARTY_AUTH_ERROR"},
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
	g.setRespond(func(w http.ResponseWriter) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"name":"projects/ai-reply/messages/2"}`)
	})
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
	g.setRespond(func(w http.ResponseWriter) { <-release })

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
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(ec)
	if err != nil {
		t.Fatal(err)
	}
	ecKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	if _, err := NewFCM(FCMConfig{ProjectID: "p", ClientEmail: "a@b", PrivateKey: ecKey}); err == nil {
		t.Fatal("an EC key is not a service-account key")
	}
	if _, err := NewFCM(FCMConfig{ProjectID: "p", ClientEmail: "not-an-email", PrivateKey: ecKey}); err == nil {
		t.Fatal("the client e-mail is checked")
	}
}

func TestPayloadSizesStayUnderProviderLimits(t *testing.T) {
	msg := testMessage()
	msg.Body = strings.Repeat("ә", 400)
	msg.Title = strings.Repeat("ә", 80)
	fcm, apns := PayloadSizes(msg)
	if fcm > MaxFCMPayloadBytes || apns > MaxAPNsPayloadBytes {
		t.Fatalf("sizes fcm=%d apns=%d", fcm, apns)
	}
	if fcm <= apns {
		t.Fatalf("the FCM request carries the APNs part and more: fcm=%d apns=%d", fcm, apns)
	}
	msg.Data["extra"] = strings.Repeat("x", 4000)
	if fcm, apns := PayloadSizes(msg); fcm <= MaxFCMPayloadBytes || apns <= MaxAPNsPayloadBytes {
		t.Fatalf("oversized data must show up: fcm=%d apns=%d", fcm, apns)
	}
}

func TestTokenHashIsStableHex(t *testing.T) {
	if TokenHash("abc") != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal("sha256 mismatch")
	}
}
