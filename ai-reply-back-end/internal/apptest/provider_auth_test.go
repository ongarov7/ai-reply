package apptest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/auth/idtoken"
)

const (
	testGoogleAudience = "123-web.apps.googleusercontent.com"
	testAppleAudience  = "kz.ai-reply.reply.keyboard.keyboard" // the iOS app's bundle id
)

// identityProviders — тесттегі Google мен Apple: өз кілттерімен қол қояды,
// сервер оларды нақты провайдерлердің кілттері сияқты тексереді.
type identityProviders struct {
	t      *testing.T
	h      *harness
	google *rsa.PrivateKey
	apple  *rsa.PrivateKey
}

func withIdentityProviders(t *testing.T, h *harness) *identityProviders {
	t.Helper()
	p := &identityProviders{t: t, h: h}
	var err error
	if p.google, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		t.Fatal(err)
	}
	if p.apple, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return h.clock.Now() }
	google, err := idtoken.New(idtoken.Config{Issuers: idtoken.GoogleIssuers,
		Audiences: []string{"123-ios.apps.googleusercontent.com", testGoogleAudience},
		Keys:      idtoken.StaticKeys{"g1": &p.google.PublicKey}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	apple, err := idtoken.New(idtoken.Config{Issuers: idtoken.AppleIssuers, Audiences: []string{testAppleAudience},
		Keys: idtoken.StaticKeys{"a1": &p.apple.PublicKey}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	h.authSvc.WithIdentityProviders(google, apple)
	return p
}

func (p *identityProviders) sign(key *rsa.PrivateKey, kid string, claims map[string]any) string {
	p.t.Helper()
	enc := func(v any) string {
		raw, err := json.Marshal(v)
		if err != nil {
			p.t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	signing := enc(map[string]any{"alg": "RS256", "kid": kid, "typ": "JWT"}) + "." + enc(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		p.t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (p *identityProviders) googleToken(sub, address string, extra map[string]any) string {
	now := p.h.clock.Now()
	claims := map[string]any{
		"iss": "https://accounts.google.com", "aud": testGoogleAudience, "azp": "123-android.apps.googleusercontent.com",
		"sub": sub, "email": address, "email_verified": true, "name": "Test User",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "nonce": "google-nonce",
	}
	for k, v := range extra {
		if v == nil {
			delete(claims, k)
		} else {
			claims[k] = v
		}
	}
	return p.sign(p.google, "g1", claims)
}

func (p *identityProviders) appleToken(sub, address, rawNonce string, extra map[string]any) string {
	now := p.h.clock.Now()
	claims := map[string]any{
		"iss": "https://appleid.apple.com", "aud": testAppleAudience, "sub": sub,
		"iat": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(), "nonce": idtoken.AppleNonce(rawNonce),
		"email_verified": "true", "nonce_supported": true,
	}
	if address != "" {
		claims["email"] = address
	}
	for k, v := range extra {
		claims[k] = v
	}
	return p.sign(p.apple, "a1", claims)
}

func (h *harness) googleSignIn(token, nonce string) response {
	return h.do(http.MethodPost, "/api/v1/auth/google",
		map[string]any{"id_token": token, "nonce": nonce, "device": device}, nil)
}

func (h *harness) appleSignIn(token, rawNonce, fullName string) response {
	return h.do(http.MethodPost, "/api/v1/auth/apple", map[string]any{
		"identity_token": token, "nonce": rawNonce, "full_name": fullName, "device": device,
	}, nil)
}

func providers(res response) []any {
	user, _ := res.body["user"].(map[string]any)
	list, _ := user["auth_providers"].([]any)
	return list
}

// Flow D: Google токені серверде тексеріледі, тіркелгі `sub` бойынша табылады.
func TestGoogleSignInCreatesThenReusesTheAccount(t *testing.T) {
	h := newHarness(t)
	idp := withIdentityProviders(t, h)

	first := h.googleSignIn(idp.googleToken("g-100", "Someone@Gmail.com", nil), "google-nonce")
	mustStatus(t, first, http.StatusOK, "")
	if isNew, _ := first.body["is_new_user"].(bool); !isNew {
		t.Fatal("first Google sign-in must create the account")
	}
	if first.str("user", "email") != "someone@gmail.com" || first.str("profile", "display_name") != "Test User" {
		t.Fatalf("first = %s", first.raw)
	}
	if list := providers(first); len(list) != 2 || list[0] != "email" || list[1] != "google" {
		t.Fatalf("auth_providers = %v", list)
	}

	// The e-mail may change at Google; the subject is what identifies the account.
	h.clock.Advance(time.Hour)
	again := h.googleSignIn(idp.googleToken("g-100", "renamed@gmail.com", nil), "google-nonce")
	mustStatus(t, again, http.StatusOK, "")
	if again.str("user", "id") != first.str("user", "id") {
		t.Fatal("the same Google subject must reach the same account")
	}
}

// Gmail — Google беделді домені: бар расталған тіркелгіге қосылады.
func TestGoogleSignInLinksToAVerifiedGmailAccount(t *testing.T) {
	h := newHarness(t)
	mailer := withMailer(h)
	idp := withIdentityProviders(t, h)

	mustStatus(t, h.requestEmailCode("aigerim@gmail.com"), http.StatusOK, "")
	emailSession := h.verifyEmailCode("aigerim@gmail.com", mailer.last(t).Code)
	mustStatus(t, emailSession, http.StatusOK, "")

	google := h.googleSignIn(idp.googleToken("g-200", "aigerim@gmail.com", nil), "google-nonce")
	mustStatus(t, google, http.StatusOK, "")
	if google.str("user", "id") != emailSession.str("user", "id") {
		t.Fatal("a verified Gmail address must link, not duplicate")
	}
	if isNew, _ := google.body["is_new_user"].(bool); isNew {
		t.Fatal("linking is not a new account")
	}
	if list := providers(google); len(list) != 2 || list[0] != "email" || list[1] != "google" {
		t.Fatalf("auth_providers = %v", list)
	}
}

func TestGoogleWorkspaceAddressLinks(t *testing.T) {
	h := newHarness(t)
	mailer := withMailer(h)
	idp := withIdentityProviders(t, h)
	mustStatus(t, h.requestEmailCode("ceo@acme.kz"), http.StatusOK, "")
	emailSession := h.verifyEmailCode("ceo@acme.kz", mailer.last(t).Code)
	google := h.googleSignIn(idp.googleToken("g-300", "ceo@acme.kz", map[string]any{"hd": "acme.kz"}), "google-nonce")
	mustStatus(t, google, http.StatusOK, "")
	if google.str("user", "id") != emailSession.str("user", "id") {
		t.Fatal("a Workspace address (hd matches) must link")
	}
}

// Google беделді емес пошта (Gmail емес, hd жоқ) басқа тіркелгіде тұрса: қосылмайды,
// екінші тіркелгі де ашылмайды — пайдаланушы поштамен кіреді.
func TestGoogleSignInDoesNotTakeOverNonAuthoritativeAddresses(t *testing.T) {
	h := newHarness(t)
	mailer := withMailer(h)
	idp := withIdentityProviders(t, h)
	mustStatus(t, h.requestEmailCode("owner@example.com"), http.StatusOK, "")
	mustStatus(t, h.verifyEmailCode("owner@example.com", mailer.last(t).Code), http.StatusOK, "")

	mustStatus(t, h.googleSignIn(idp.googleToken("g-400", "owner@example.com", nil), "google-nonce"),
		http.StatusConflict, "EMAIL_ALREADY_IN_USE")
	var identities, users int
	_ = h.db.Reader().QueryRow(`SELECT COUNT(*) FROM auth_identities WHERE kind = 'google'`).Scan(&identities)
	_ = h.db.Reader().QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users)
	if identities != 0 || users != 1 {
		t.Fatalf("identities=%d users=%d: nothing may be linked or created", identities, users)
	}

	// An unverified provider address is never used, not even a Gmail one.
	mustStatus(t, h.googleSignIn(idp.googleToken("g-401", "fresh@gmail.com",
		map[string]any{"email_verified": false}), "google-nonce"), http.StatusUnauthorized, "INVALID_ID_TOKEN")
}

// Беделді емес, бірақ бос пошта: тіркелгі сол поштамен ашылады, кейін поштаны
// OTP-мен дәлелдеген адам сол тіркелгіге кіреді (пошта — қалпына келтіру арнасы).
func TestProviderAccountKeepsItsAddressForEmailSignIn(t *testing.T) {
	h := newHarness(t)
	mailer := withMailer(h)
	idp := withIdentityProviders(t, h)

	google := h.googleSignIn(idp.googleToken("g-600", "person@mail.ru", nil), "google-nonce")
	mustStatus(t, google, http.StatusOK, "")
	if google.str("user", "email") != "person@mail.ru" {
		t.Fatalf("user = %s", google.raw)
	}
	if list := providers(google); len(list) != 1 || list[0] != "google" {
		t.Fatalf("auth_providers = %v: an address Google is not authoritative for is not yet an e-mail sign-in", list)
	}

	mustStatus(t, h.requestEmailCode("person@mail.ru"), http.StatusOK, "")
	session := h.verifyEmailCode("person@mail.ru", mailer.last(t).Code)
	mustStatus(t, session, http.StatusOK, "")
	if session.str("user", "id") != google.str("user", "id") {
		t.Fatal("proving the mailbox must reach the account that holds it")
	}
	if list := providers(session); len(list) != 2 || list[0] != "email" || list[1] != "google" {
		t.Fatalf("auth_providers = %v", list)
	}
}

// Apple (Gmail үшін беделді емес) ашқан тіркелгіні Google (беделді) кейін өзіне қосады.
func TestAuthoritativeProviderLinksIntoTheAccountHoldingTheAddress(t *testing.T) {
	h := newHarness(t)
	idp := withIdentityProviders(t, h)

	apple := h.appleSignIn(idp.appleToken("001234.first.0004", "both@gmail.com", "n-1", nil), "n-1", "")
	mustStatus(t, apple, http.StatusOK, "")
	if list := providers(apple); len(list) != 1 || list[0] != "apple" {
		t.Fatalf("auth_providers = %v", list)
	}
	google := h.googleSignIn(idp.googleToken("g-700", "both@gmail.com", nil), "google-nonce")
	mustStatus(t, google, http.StatusOK, "")
	if google.str("user", "id") != apple.str("user", "id") {
		t.Fatal("Google proves the Gmail mailbox and must reach the account holding it")
	}
	if isNew, _ := google.body["is_new_user"].(bool); isNew {
		t.Fatal("linking is not a new account")
	}
	if list := providers(google); len(list) != 3 {
		t.Fatalf("auth_providers = %v, want apple, email and google", list)
	}
}

func TestAppleSignInWithAnAddressOfAnotherAccountIsRefused(t *testing.T) {
	h := newHarness(t)
	mailer := withMailer(h)
	idp := withIdentityProviders(t, h)
	mustStatus(t, h.requestEmailCode("dana@gmail.com"), http.StatusOK, "")
	mustStatus(t, h.verifyEmailCode("dana@gmail.com", mailer.last(t).Code), http.StatusOK, "")
	mustStatus(t, h.appleSignIn(idp.appleToken("001234.gmail.0003", "dana@gmail.com", "n", nil), "n", ""),
		http.StatusConflict, "EMAIL_ALREADY_IN_USE")
}

func TestGoogleSignInRejectsBadTokens(t *testing.T) {
	h := newHarness(t)
	idp := withIdentityProviders(t, h)
	forged, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	valid := idp.googleToken("g-500", "x@gmail.com", nil)
	cases := map[string]response{
		"wrong audience": h.googleSignIn(idp.googleToken("g-500", "x@gmail.com",
			map[string]any{"aud": "someone-elses-app.apps.googleusercontent.com"}), "google-nonce"),
		"wrong issuer": h.googleSignIn(idp.googleToken("g-500", "x@gmail.com",
			map[string]any{"iss": "https://evil.example"}), "google-nonce"),
		"expired": h.googleSignIn(idp.googleToken("g-500", "x@gmail.com",
			map[string]any{"exp": h.clock.Now().Add(-10 * time.Minute).Unix()}), "google-nonce"),
		"forged signature": h.googleSignIn(idp.sign(forged, "g1", map[string]any{
			"iss": "https://accounts.google.com", "aud": testGoogleAudience, "sub": "g-500",
			"exp": h.clock.Now().Add(time.Hour).Unix(), "nonce": "google-nonce"}), "google-nonce"),
		"nonce mismatch": h.googleSignIn(valid, "another-nonce"),
		"missing nonce":  h.googleSignIn(valid, ""),
		"garbage":        h.googleSignIn("not.a.token", "google-nonce"),
	}
	for name, res := range cases {
		if res.status != http.StatusUnauthorized || res.errorCode() != "INVALID_ID_TOKEN" {
			t.Errorf("%s: got %d %s, want 401 INVALID_ID_TOKEN", name, res.status, res.errorCode())
		}
	}
	// Client-supplied identity fields are not accepted at all.
	extra := h.do(http.MethodPost, "/api/v1/auth/google", map[string]any{
		"id_token": valid, "nonce": "google-nonce", "email": "victim@gmail.com", "user_id": "x",
	}, nil)
	mustStatus(t, extra, http.StatusBadRequest, "INVALID_REQUEST")
}

// Flow E: Apple, Hide My Email, аты тек бірінші рет келеді.
func TestAppleSignInWithPrivateRelay(t *testing.T) {
	h := newHarness(t)
	idp := withIdentityProviders(t, h)
	relay := "x7k2mq9p4t@privaterelay.appleid.com"

	first := h.appleSignIn(idp.appleToken("001234.abcdef.0987", relay, "raw-nonce-1",
		map[string]any{"is_private_email": "true"}), "raw-nonce-1", "Айгерім Сейітқызы")
	mustStatus(t, first, http.StatusOK, "")
	if isNew, _ := first.body["is_new_user"].(bool); !isNew {
		t.Fatal("first Apple sign-in must create the account")
	}
	if first.str("user", "email") != relay || first.str("profile", "display_name") != "Айгерім Сейітқызы" {
		t.Fatalf("first = %s", first.raw)
	}

	// Later sign-ins carry no name, sometimes no e-mail; the subject decides.
	h.clock.Advance(24 * time.Hour)
	again := h.appleSignIn(idp.appleToken("001234.abcdef.0987", "", "raw-nonce-2", nil), "raw-nonce-2", "Someone Else")
	mustStatus(t, again, http.StatusOK, "")
	if again.str("user", "id") != first.str("user", "id") {
		t.Fatal("the same Apple subject must reach the same account")
	}
	if again.str("profile", "display_name") != "Айгерім Сейітқызы" {
		t.Fatal("a later name must not overwrite the profile")
	}
}

func TestAppleSignInRequiresTheMatchingNonce(t *testing.T) {
	h := newHarness(t)
	idp := withIdentityProviders(t, h)
	token := idp.appleToken("001234.nonce.0001", "someone@icloud.com", "raw-nonce", nil)
	mustStatus(t, h.appleSignIn(token, "a-different-nonce", ""), http.StatusUnauthorized, "INVALID_ID_TOKEN")
	mustStatus(t, h.appleSignIn(token, "", ""), http.StatusUnauthorized, "INVALID_ID_TOKEN")
	mustStatus(t, h.appleSignIn(token, "raw-nonce", ""), http.StatusOK, "")
}

// Apple-дың өз домені (icloud.com) бар расталған тіркелгіге қосылады.
func TestAppleSignInLinksAnICloudAccount(t *testing.T) {
	h := newHarness(t)
	mailer := withMailer(h)
	idp := withIdentityProviders(t, h)
	mustStatus(t, h.requestEmailCode("dana@icloud.com"), http.StatusOK, "")
	emailSession := h.verifyEmailCode("dana@icloud.com", mailer.last(t).Code)
	apple := h.appleSignIn(idp.appleToken("001234.icloud.0002", "dana@icloud.com", "n", nil), "n", "")
	mustStatus(t, apple, http.StatusOK, "")
	if apple.str("user", "id") != emailSession.str("user", "id") {
		t.Fatal("a verified iCloud address must link")
	}
}

func TestProviderSignInIsUnavailableWithoutConfiguration(t *testing.T) {
	h := newHarness(t)
	mustStatus(t, h.googleSignIn("x.y.z", "n"), http.StatusServiceUnavailable, "AUTH_PROVIDER_UNAVAILABLE")
	mustStatus(t, h.appleSignIn("x.y.z", "n", ""), http.StatusServiceUnavailable, "AUTH_PROVIDER_UNAVAILABLE")

	config := h.do(http.MethodGet, "/api/v1/config", nil, nil)
	features, _ := config.body["features"].(map[string]any)
	if features["email_otp"] != true || features["google_sign_in"] != false || features["apple_sign_in"] != false {
		t.Fatalf("features = %v", features)
	}
	withIdentityProviders(t, h)
	config = h.do(http.MethodGet, "/api/v1/config", nil, nil)
	features, _ = config.body["features"].(map[string]any)
	if features["google_sign_in"] != true || features["apple_sign_in"] != true {
		t.Fatalf("features = %v", features)
	}
}

// Бір провайдер тіркелгісімен параллель алғашқы кірулер бір ғана тіркелгі ашады.
func TestProviderIdentityIsUniqueUnderConcurrency(t *testing.T) {
	h := newHarness(t)
	idp := withIdentityProviders(t, h)
	token := idp.googleToken("g-race", "racer@gmail.com", nil)

	const workers = 8
	ids := make([]string, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			res := h.googleSignIn(token, "google-nonce")
			if res.status == http.StatusOK {
				ids[i] = res.str("user", "id")
			}
		}(i)
	}
	close(start)
	wg.Wait()
	for _, id := range ids {
		if id == "" || id != ids[0] {
			t.Fatalf("concurrent first sign-ins produced %v", ids)
		}
	}
	var users, identities int
	_ = h.db.Reader().QueryRow(`SELECT COUNT(*) FROM users WHERE email = 'racer@gmail.com'`).Scan(&users)
	_ = h.db.Reader().QueryRow(`SELECT COUNT(*) FROM auth_identities WHERE kind = 'google' AND value = 'g-race'`).Scan(&identities)
	if users != 1 || identities != 1 {
		t.Fatalf("users=%d identities=%d, want 1 and 1", users, identities)
	}
}

func TestDisabledAccountCannotSignInWithAProvider(t *testing.T) {
	h := newHarness(t)
	idp := withIdentityProviders(t, h)
	first := h.googleSignIn(idp.googleToken("g-off", "off@gmail.com", nil), "google-nonce")
	mustStatus(t, first, http.StatusOK, "")
	if _, err := h.db.Writer().Exec(`UPDATE users SET status = 'disabled' WHERE id = ?`, first.str("user", "id")); err != nil {
		t.Fatal(err)
	}
	mustStatus(t, h.googleSignIn(idp.googleToken("g-off", "off@gmail.com", nil), "google-nonce"),
		http.StatusForbidden, "ACCOUNT_DISABLED")
}
