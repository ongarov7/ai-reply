package googleauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// Токен кэштеледі, мерзімі бітуге 5 минут қалғанда және Forget-тен кейін жаңасы алынады.
func TestTokenIsCachedRefreshedAndForgotten(t *testing.T) {
	key, _ := testKey(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || strings.Count(r.Form.Get("assertion"), ".") != 2 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"ya29.a","expires_in":3600}`)
	}))
	defer server.Close()
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	src, err := New(Config{ClientEmail: "sa@p.iam.gserviceaccount.com", Key: key, Scope: "scope",
		TokenURL: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if tok, err := src.Token(context.Background()); err != nil || tok != "ya29.a" {
			t.Fatalf("token: %q %v", tok, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("calls %d, want a cached token", calls.Load())
	}
	now = now.Add(56 * time.Minute)
	_, _ = src.Token(context.Background())
	src.Forget()
	_, _ = src.Token(context.Background())
	if calls.Load() != 3 {
		t.Fatalf("calls %d, want a refresh near expiry and after Forget", calls.Load())
	}
}

func TestRefusalsNeverEchoTheKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
	}))
	defer server.Close()
	key, _ := testKey(t)
	src, _ := New(Config{ClientEmail: "sa@p", Key: key, Scope: "s", TokenURL: server.URL, HTTPClient: server.Client()})
	if _, err := src.Token(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTP 400 invalid_grant") {
		t.Fatalf("err = %v", err)
	}
	if _, err := ParseRSAKey("-----BEGIN PRIVATE KEY-----\nc2VjcmV0\n-----END PRIVATE KEY-----\n"); err == nil ||
		strings.Contains(err.Error(), "c2VjcmV0") {
		t.Fatalf("parse: %v", err)
	}
	if _, err := New(Config{ClientEmail: "sa@p", Scope: "s"}); err == nil {
		t.Fatal("no key accepted")
	}
}
