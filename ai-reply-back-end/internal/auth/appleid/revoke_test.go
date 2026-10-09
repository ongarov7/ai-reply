package appleid

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func testKey(t *testing.T) (*ecdsa.PrivateKey, string) {
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

// verifySecret — client_secret қолтаңбасы мен claim-дері (HTTP өңдеушісінен шақырылады: тек Errorf).
func verifySecret(t *testing.T, key *ecdsa.PrivateKey, secret, clientID string, now time.Time) {
	t.Helper()
	parts := strings.Split(secret, ".")
	if len(parts) != 3 {
		t.Errorf("client secret is not a JWT: %q", secret)
		return
	}
	var header map[string]string
	var claims map[string]any
	raw, _ := base64.RawURLEncoding.DecodeString(parts[0])
	_ = json.Unmarshal(raw, &header)
	raw, _ = base64.RawURLEncoding.DecodeString(parts[1])
	_ = json.Unmarshal(raw, &claims)
	if header["alg"] != "ES256" || header["kid"] != "KEY123" {
		t.Errorf("header = %v", header)
		return
	}
	if claims["iss"] != "TEAM123" || claims["sub"] != clientID || claims["aud"] != "https://appleid.apple.com" ||
		claims["iat"] != float64(now.Unix()) {
		t.Errorf("claims = %v", claims)
		return
	}
	if exp := claims["exp"].(float64); exp <= float64(now.Unix()) || exp > float64(now.Add(180*24*time.Hour).Unix()) {
		t.Errorf("exp = %v", exp)
		return
	}
	signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
	if len(signature) != 64 {
		t.Errorf("signature is %d bytes, want raw r||s", len(signature))
		return
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])
	if !ecdsa.Verify(&key.PublicKey, digest[:], r, s) {
		t.Error("client secret signature does not verify")
	}
}

type fakeApple struct {
	mu       sync.Mutex
	calls    []string
	revoked  []string
	rejectID string // client id whose exchange fails with invalid_client
	failWith int    // status for /auth/revoke
}

func (f *fakeApple) handler(t *testing.T, key *ecdsa.PrivateKey, now time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("form: %v", err)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, r.URL.Path+" "+r.Form.Get("client_id"))
		verifySecret(t, key, r.Form.Get("client_secret"), r.Form.Get("client_id"), now)
		switch r.URL.Path {
		case "/auth/token":
			if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "auth-code-1" {
				t.Errorf("token form = %v", r.Form)
			}
			if r.Form.Get("client_id") == f.rejectID {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"at-1","token_type":"Bearer","expires_in":3600,"refresh_token":"rt-1","id_token":"x"}`))
		case "/auth/revoke":
			if r.Form.Get("token") != "rt-1" || r.Form.Get("token_type_hint") != "refresh_token" {
				t.Errorf("revoke form = %v", r.Form)
			}
			if f.failWith != 0 {
				w.WriteHeader(f.failWith)
				_, _ = w.Write([]byte(`{"error":"invalid_request"}`))
				return
			}
			f.revoked = append(f.revoked, r.Form.Get("token"))
		default:
			http.NotFound(w, r)
		}
	})
}

func newTestRevoker(t *testing.T, apple *fakeApple, clientIDs ...string) *Revoker {
	t.Helper()
	key, pemKey := testKey(t)
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	server := httptest.NewServer(apple.handler(t, key, now))
	t.Cleanup(server.Close)
	revoker, err := New(Config{
		TeamID: "TEAM123", KeyID: "KEY123", PrivateKey: pemKey, ClientIDs: clientIDs,
		BaseURL: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return revoker
}

func TestRevokeExchangesTheCodeAndRevokesTheRefreshToken(t *testing.T) {
	apple := &fakeApple{}
	revoker := newTestRevoker(t, apple, "kz.ai-reply.app")
	if err := revoker.Revoke(context.Background(), " auth-code-1 "); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if strings.Join(apple.calls, ",") != "/auth/token kz.ai-reply.app,/auth/revoke kz.ai-reply.app" {
		t.Fatalf("calls = %v", apple.calls)
	}
	if len(apple.revoked) != 1 {
		t.Fatalf("revoked = %v", apple.revoked)
	}
}

// Код қай client id-ге берілгені белгісіз болса, келесісі сыналады.
func TestRevokeTriesEachClientID(t *testing.T) {
	apple := &fakeApple{rejectID: "kz.other"}
	revoker := newTestRevoker(t, apple, "kz.other", "kz.ai-reply.app")
	if err := revoker.Revoke(context.Background(), "auth-code-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if len(apple.revoked) != 1 || apple.calls[len(apple.calls)-1] != "/auth/revoke kz.ai-reply.app" {
		t.Fatalf("calls = %v", apple.calls)
	}
}

func TestRevokeFailureNamesOnlyTheStatus(t *testing.T) {
	apple := &fakeApple{failWith: http.StatusBadRequest}
	revoker := newTestRevoker(t, apple, "kz.ai-reply.app")
	err := revoker.Revoke(context.Background(), "auth-code-1")
	if !errors.Is(err, ErrRevoke) {
		t.Fatalf("err = %v", err)
	}
	for _, secret := range []string{"auth-code-1", "rt-1", "at-1", "eyJ"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error %q leaks %q", err, secret)
		}
	}
	if !strings.Contains(err.Error(), "400 invalid_request") {
		t.Fatalf("error %q does not name the status", err)
	}
	if err := revoker.Revoke(context.Background(), " "); !errors.Is(err, ErrRevoke) {
		t.Fatalf("empty code: %v", err)
	}
}

func TestNewRejectsBadKeys(t *testing.T) {
	_, pemKey := testKey(t)
	rsaLike := strings.Replace(pemKey, "PRIVATE KEY", "RSA PRIVATE KEY", -1)
	for name, cfg := range map[string]Config{
		"no team":      {KeyID: "K", PrivateKey: pemKey, ClientIDs: []string{"a"}},
		"no client id": {TeamID: "T", KeyID: "K", PrivateKey: pemKey},
		"not pem":      {TeamID: "T", KeyID: "K", PrivateKey: "MIGT...", ClientIDs: []string{"a"}},
		"broken der":   {TeamID: "T", KeyID: "K", PrivateKey: rsaLike[:60] + "\n-----END RSA PRIVATE KEY-----", ClientIDs: []string{"a"}},
	} {
		if _, err := New(cfg); err == nil {
			t.Fatalf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "MIG") {
			t.Fatalf("%s: error leaks the key: %v", name, err)
		}
	}
}
