package idtoken

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testNow = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func newKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func b64(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func sign(t *testing.T, key *rsa.PrivateKey, header, claims map[string]any) string {
	t.Helper()
	signing := b64(header) + "." + b64(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func googleClaims() map[string]any {
	return map[string]any{
		"iss": "https://accounts.google.com", "aud": "web-client", "azp": "android-client",
		"sub": "1234567890", "email": "someone@gmail.com", "email_verified": true,
		"iat": testNow.Add(-time.Minute).Unix(), "exp": testNow.Add(time.Hour).Unix(),
		"nonce": "n-123", "name": "Someone",
	}
}

func verifier(t *testing.T, keys KeySource) *Verifier {
	t.Helper()
	v, err := New(Config{Issuers: GoogleIssuers, Audiences: []string{"ios-client", "web-client"},
		Keys: keys, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	return v
}

func TestVerifyAcceptsValidToken(t *testing.T) {
	key := newKey(t)
	v := verifier(t, StaticKeys{"k1": &key.PublicKey})
	token := sign(t, key, map[string]any{"alg": "RS256", "kid": "k1", "typ": "JWT"}, googleClaims())

	claims, err := v.Verify(context.Background(), token, "n-123")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Subject != "1234567890" || claims.Email != "someone@gmail.com" || !claims.EmailVerified {
		t.Fatalf("claims = %+v", claims)
	}
	if claims.AuthorizedParty != "android-client" || claims.Name != "Someone" {
		t.Fatalf("claims = %+v", claims)
	}
	// An empty expected nonce skips the check.
	if _, err := v.Verify(context.Background(), token, ""); err != nil {
		t.Fatalf("verify without nonce: %v", err)
	}
}

func TestVerifyRejectsTamperedAndMisissuedTokens(t *testing.T) {
	key := newKey(t)
	other := newKey(t)
	v := verifier(t, StaticKeys{"k1": &key.PublicKey})
	header := map[string]any{"alg": "RS256", "kid": "k1"}

	with := func(mutate func(map[string]any)) map[string]any {
		c := googleClaims()
		mutate(c)
		return c
	}

	cases := map[string]string{
		"wrong issuer":    sign(t, key, header, with(func(c map[string]any) { c["iss"] = "https://evil.example" })),
		"wrong audience":  sign(t, key, header, with(func(c map[string]any) { c["aud"] = "someone-else" })),
		"expired":         sign(t, key, header, with(func(c map[string]any) { c["exp"] = testNow.Add(-2 * time.Minute).Unix() })),
		"missing expiry":  sign(t, key, header, with(func(c map[string]any) { delete(c, "exp") })),
		"future iat":      sign(t, key, header, with(func(c map[string]any) { c["iat"] = testNow.Add(10 * time.Minute).Unix() })),
		"not yet valid":   sign(t, key, header, with(func(c map[string]any) { c["nbf"] = testNow.Add(10 * time.Minute).Unix() })),
		"missing subject": sign(t, key, header, with(func(c map[string]any) { delete(c, "sub") })),
		"nonce mismatch":  sign(t, key, header, with(func(c map[string]any) { c["nonce"] = "other" })),
		"other key":       sign(t, other, header, googleClaims()),
		"unknown kid":     sign(t, key, map[string]any{"alg": "RS256", "kid": "k9"}, googleClaims()),
		"missing kid":     sign(t, key, map[string]any{"alg": "RS256"}, googleClaims()),
		"alg none":        b64(map[string]any{"alg": "none", "kid": "k1"}) + "." + b64(googleClaims()) + ".",
		"alg hs256":       b64(map[string]any{"alg": "HS256", "kid": "k1"}) + "." + b64(googleClaims()) + ".c2ln",
		"two segments":    b64(header) + "." + b64(googleClaims()),
		"garbage":         "not-a-token",
		"empty":           "",
		"oversized":       strings.Repeat("a", maxTokenBytes+1),
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := v.Verify(context.Background(), token, "n-123"); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("err = %v, want ErrInvalidToken", err)
			}
		})
	}

	// A valid token whose payload was edited after signing.
	valid := sign(t, key, header, googleClaims())
	parts := strings.Split(valid, ".")
	edited := googleClaims()
	edited["sub"] = "attacker"
	tampered := parts[0] + "." + b64(edited) + "." + parts[2]
	if _, err := v.Verify(context.Background(), tampered, "n-123"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("tampered payload accepted: %v", err)
	}
}

func TestVerifyToleratesSmallClockSkew(t *testing.T) {
	key := newKey(t)
	v := verifier(t, StaticKeys{"k1": &key.PublicKey})
	claims := googleClaims()
	claims["exp"] = testNow.Add(-30 * time.Second).Unix()
	claims["iat"] = testNow.Add(30 * time.Second).Unix()
	if _, err := v.Verify(context.Background(), sign(t, key, map[string]any{"alg": "RS256", "kid": "k1"}, claims), ""); err != nil {
		t.Fatalf("30s of skew must be tolerated: %v", err)
	}
}

func TestAppleClaimShapes(t *testing.T) {
	key := newKey(t)
	v, err := New(Config{Issuers: AppleIssuers, Audiences: []string{"kz.yerek.replykeyboard"},
		Keys: StaticKeys{"a1": &key.PublicKey}, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatal(err)
	}
	raw := "raw-nonce-value"
	token := sign(t, key, map[string]any{"alg": "RS256", "kid": "a1"}, map[string]any{
		"iss": "https://appleid.apple.com", "aud": []string{"kz.yerek.replykeyboard"},
		"sub": "001234.abcdef.0987", "exp": float64(testNow.Add(10 * time.Minute).Unix()),
		"iat": testNow.Unix(), "nonce": AppleNonce(raw), "email": "x7k2@privaterelay.appleid.com",
		"email_verified": "true", "is_private_email": "true",
	})
	claims, err := v.Verify(context.Background(), token, AppleNonce(raw))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !claims.EmailVerified || !claims.IsPrivateEmail || claims.Subject != "001234.abcdef.0987" {
		t.Fatalf("claims = %+v", claims)
	}
	if _, err := v.Verify(context.Background(), token, AppleNonce("different")); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("wrong raw nonce accepted: %v", err)
	}
}

func TestAppleNonceIsSHA256Hex(t *testing.T) {
	if got := AppleNonce("abc"); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("AppleNonce(abc) = %s", got)
	}
}

func TestNewRequiresConfiguration(t *testing.T) {
	key := newKey(t)
	for name, cfg := range map[string]Config{
		"no issuers":   {Audiences: []string{"a"}, Keys: StaticKeys{"k": &key.PublicKey}},
		"no audiences": {Issuers: GoogleIssuers, Audiences: []string{" "}, Keys: StaticKeys{"k": &key.PublicKey}},
		"no keys":      {Issuers: GoogleIssuers, Audiences: []string{"a"}},
	} {
		if _, err := New(cfg); err == nil {
			t.Fatalf("%s: New accepted an unusable configuration", name)
		}
	}
}

// ---------------------------------------------------------------- JWKS

func jwk(kid string, key *rsa.PublicKey) map[string]any {
	return map[string]any{
		"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}
}

type jwksServer struct {
	*httptest.Server
	hits atomic.Int32
	keys atomic.Value // []map[string]any
	fail atomic.Bool
}

func newJWKSServer(t *testing.T, keys ...map[string]any) *jwksServer {
	s := &jwksServer{}
	s.keys.Store(keys)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		if s.fail.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=600, must-revalidate")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": s.keys.Load()})
	}))
	t.Cleanup(s.Close)
	return s
}

func TestRemoteKeysCacheRotationAndOutage(t *testing.T) {
	first, second := newKey(t), newKey(t)
	server := newJWKSServer(t, jwk("k1", &first.PublicKey))
	now := testNow
	keys := NewRemoteKeys(server.URL, server.Client())
	keys.now = func() time.Time { return now }
	ctx := context.Background()

	if _, err := keys.PublicKey(ctx, "k1"); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if _, err := keys.PublicKey(ctx, "k1"); err != nil || server.hits.Load() != 1 {
		t.Fatalf("cached key must not refetch: hits=%d err=%v", server.hits.Load(), err)
	}

	// The provider rotates: an unknown kid refetches, but not more than once
	// per interval, so made-up kids cannot hammer the provider.
	server.keys.Store([]map[string]any{jwk("k1", &first.PublicKey), jwk("k2", &second.PublicKey)})
	if _, err := keys.PublicKey(ctx, "k2"); err == nil || server.hits.Load() != 1 {
		t.Fatalf("refetch inside the throttle window: hits=%d err=%v", server.hits.Load(), err)
	}
	now = now.Add(refetchInterval)
	if _, err := keys.PublicKey(ctx, "k2"); err != nil || server.hits.Load() != 2 {
		t.Fatalf("rotated key: hits=%d err=%v", server.hits.Load(), err)
	}
	now = now.Add(refetchInterval)
	if _, err := keys.PublicKey(ctx, "bogus"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("unknown kid after refetch: %v", err)
	}

	// Past max-age the set refreshes; if the provider is down, the last known
	// keys keep working instead of locking every user out.
	now = now.Add(11 * time.Minute)
	server.fail.Store(true)
	if _, err := keys.PublicKey(ctx, "k1"); err != nil {
		t.Fatalf("stale key during outage: %v", err)
	}
}

func TestRemoteKeysUnavailableWithoutKeys(t *testing.T) {
	server := newJWKSServer(t)
	server.fail.Store(true)
	keys := NewRemoteKeys(server.URL, server.Client())
	if _, err := keys.PublicKey(context.Background(), "k1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	// Surfaces as unavailable through the verifier too, not as a bad token.
	v, _ := New(Config{Issuers: GoogleIssuers, Audiences: []string{"web-client"}, Keys: keys})
	key := newKey(t)
	token := sign(t, key, map[string]any{"alg": "RS256", "kid": "k1"}, googleClaims())
	if _, err := v.Verify(context.Background(), token, ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("verify err = %v, want ErrUnavailable", err)
	}
}

func TestParseJWKSSkipsUnusableKeys(t *testing.T) {
	good := newKey(t)
	weak, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	enc := jwk("enc", &good.PublicKey)
	enc["use"] = "enc"
	body, _ := json.Marshal(map[string]any{"keys": []map[string]any{
		jwk("good", &good.PublicKey), jwk("weak", &weak.PublicKey), enc,
		{"kty": "EC", "kid": "ec"},
	}})
	keys, err := ParseJWKS(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys["good"] == nil {
		t.Fatalf("keys = %v, want only the 2048-bit signing key", keys)
	}
	if _, err := ParseJWKS([]byte(`{"keys":[]}`)); err == nil {
		t.Fatal("an empty key set must be an error")
	}
}

func TestCacheTTL(t *testing.T) {
	cases := map[string]time.Duration{
		"":                              defaultKeysTTL,
		"public, max-age=19800":         19800 * time.Second,
		"max-age=10":                    minKeysTTL,
		"max-age=999999":                maxKeysTTL,
		"no-cache, max-age=nonsense":    defaultKeysTTL,
		"public, max-age=600, must-rev": 10 * time.Minute,
	}
	for header, want := range cases {
		if got := cacheTTL(header); got != want {
			t.Errorf("cacheTTL(%q) = %v, want %v", header, got, want)
		}
	}
}
