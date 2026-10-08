package apptest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/auth/idtoken"
)

// rejections — журналдағы «identity token rejected» жолдары (JSON өрістерімен).
func (h *harness) rejections() []map[string]any {
	h.t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(h.logs.String(), "\n") {
		if !strings.Contains(line, `"identity token rejected"`) {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			h.t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, entry)
	}
	return out
}

// iosNonce — iOS қолданбасы сияқты: 32 кездейсоқ байт base64url (сервер алатын мән)
// және Apple сұранысына баратын кіші әріпті hex SHA-256 (SignInNonce.sha256).
func iosNonce(t *testing.T) (raw, hashed string) {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(raw))
	return raw, hex.EncodeToString(sum[:])
}

// Нақты bundle id (APPLE_CLIENT_ID арқылы) және iOS-тың nonce хэші: кіру сәтті өтеді;
// ескі bundle id-мен қол қойылған токен бір нақты ескертумен қабылданбайды.
func TestAppleSignInWithTheAppBundleIDFromConfiguration(t *testing.T) {
	const bundleID = "kz.ai-reply.reply.keyboard.keyboard"
	h := newHarness(t, withEnv("APPLE_CLIENT_ID", bundleID))
	if got := strings.Join(h.cfg.OAuth.AppleClientIDs, ","); got != bundleID {
		t.Fatalf("APPLE_CLIENT_ID = %q", got)
	}
	appleKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	apple, err := idtoken.New(idtoken.Config{Issuers: idtoken.AppleIssuers, Audiences: h.cfg.OAuth.AppleClientIDs,
		Keys: idtoken.StaticKeys{"apple-1": &appleKey.PublicKey}, Now: func() time.Time { return h.clock.Now() }})
	if err != nil {
		t.Fatal(err)
	}
	h.authSvc.WithIdentityProviders(nil, apple)
	idp := &identityProviders{t: t, h: h, apple: appleKey}
	token := func(aud, nonce string) string {
		now := h.clock.Now()
		return idp.sign(appleKey, "apple-1", map[string]any{
			"iss": "https://appleid.apple.com", "aud": aud, "sub": "001234.bundle.0001",
			"iat": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(), "nonce": nonce,
			"email": "bundle.person@icloud.com", "email_verified": "true", "is_private_email": "false",
			"auth_time": now.Unix(), "nonce_supported": true,
		})
	}

	// A token for the old bundle id is refused with the precise reason.
	raw, hashed := iosNonce(t)
	old := token("kz.yerek.replykeyboard", hashed)
	mustStatus(t, h.appleSignIn(old, raw, ""), http.StatusUnauthorized, "INVALID_ID_TOKEN")
	logged := h.rejections()
	if len(logged) != 1 {
		t.Fatalf("rejections logged = %d, want 1", len(logged))
	}
	entry := logged[0]
	if entry["level"] != "WARN" || entry["provider"] != "apple" || entry["reason"] != "audience_mismatch" ||
		fmt.Sprint(entry["token_aud"]) != "[kz.yerek.replykeyboard]" || fmt.Sprint(entry["configured_aud"]) != "["+bundleID+"]" {
		t.Fatalf("rejection = %v", entry)
	}
	logs := h.logs.String()
	for _, secret := range []string{old, strings.Split(old, ".")[1], "bundle.person@icloud.com", "001234.bundle.0001", raw, hashed} {
		if strings.Contains(logs, secret) {
			t.Fatalf("the log contains %q", secret)
		}
	}

	// The app's own bundle id and the nonce hashed the way the iOS app does: signed in.
	raw, hashed = iosNonce(t)
	ok := h.appleSignIn(token(bundleID, hashed), raw, "Aigerim Seitkyzy")
	mustStatus(t, ok, http.StatusOK, "")
	if isNew, _ := ok.body["is_new_user"].(bool); !isNew || ok.str("user", "email") != "bundle.person@icloud.com" ||
		ok.str("profile", "display_name") != "Aigerim Seitkyzy" {
		t.Fatalf("sign-in = %s", ok.raw)
	}
	if n := len(h.rejections()); n != 1 {
		t.Fatalf("a successful sign-in logged a rejection (%d)", n)
	}
}

// Әр қабылданбаған токен — бір ғана ескерту, нақты себеп санатымен; HTTP жауабы өзгермейді.
func TestRejectedIdentityTokensAreLoggedOnceWithTheReason(t *testing.T) {
	h := newHarness(t)
	idp := withIdentityProviders(t, h)
	forged, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := h.clock.Now()
	appleNonce := idtoken.AppleNonce("raw-nonce")

	cases := []struct {
		name   string
		send   func() response
		reason string
		fields map[string]string
	}{
		{"apple nonce: raw value in the Apple request", func() response {
			return h.appleSignIn(idp.appleToken("001234.diag.0001", "d1@icloud.com", "", map[string]any{"nonce": "raw-nonce"}),
				"raw-nonce", "")
		}, "nonce_mismatch", map[string]string{"nonce_form": "sent_value"}},
		{"apple nonce: hash sent instead of the raw value", func() response {
			return h.appleSignIn(idp.appleToken("001234.diag.0002", "d2@icloud.com", "raw-nonce", nil), appleNonce, "")
		}, "nonce_mismatch", map[string]string{"nonce_form": "sent_value"}},
		{"apple nonce: upper-case hash", func() response {
			return h.appleSignIn(idp.appleToken("001234.diag.0003", "d3@icloud.com", "", map[string]any{
				"nonce": strings.ToUpper(appleNonce)}), "raw-nonce", "")
		}, "nonce_mismatch", map[string]string{"nonce_form": "uppercase_hash"}},
		{"apple nonce: absent", func() response {
			return h.appleSignIn(idp.appleToken("001234.diag.0004", "d4@icloud.com", "", map[string]any{"nonce": ""}),
				"raw-nonce", "")
		}, "nonce_mismatch", map[string]string{"nonce_form": "absent"}},
		{"apple nonce: another attempt", func() response {
			return h.appleSignIn(idp.appleToken("001234.diag.0005", "d5@icloud.com", "older-nonce", nil), "raw-nonce", "")
		}, "nonce_mismatch", map[string]string{"nonce_form": "different"}},
		{"apple: no nonce from the app", func() response {
			return h.appleSignIn(idp.appleToken("001234.diag.0006", "d6@icloud.com", "raw-nonce", nil), "", "")
		}, "missing_nonce", nil},
		{"apple: expired", func() response {
			return h.appleSignIn(idp.appleToken("001234.diag.0007", "d7@icloud.com", "raw-nonce", map[string]any{
				"exp": now.Add(-time.Hour).Unix()}), "raw-nonce", "")
		}, "expired", map[string]string{"seconds": "3600"}},
		{"apple: issuer", func() response {
			return h.appleSignIn(idp.appleToken("001234.diag.0008", "d8@icloud.com", "raw-nonce", map[string]any{
				"iss": "https://accounts.google.com"}), "raw-nonce", "")
		}, "issuer_mismatch", map[string]string{"token_iss": "https://accounts.google.com", "configured_iss": "[https://appleid.apple.com]"}},
		{"apple: no e-mail for a new account", func() response {
			return h.appleSignIn(idp.appleToken("001234.diag.0009", "", "raw-nonce", nil), "raw-nonce", "")
		}, "missing_email", map[string]string{"email_in_token": "false"}},
		{"google: unverified e-mail for a new account", func() response {
			return h.googleSignIn(idp.googleToken("g-diag-1", "unverified@example.com", map[string]any{"email_verified": false}),
				"google-nonce")
		}, "missing_email", map[string]string{"email_in_token": "true"}},
		{"google: hashed nonce", func() response {
			return h.googleSignIn(idp.googleToken("g-diag-2", "g2@gmail.com", map[string]any{
				"nonce": idtoken.AppleNonce("google-nonce")}), "google-nonce")
		}, "nonce_mismatch", map[string]string{"nonce_form": "hashed"}},
		{"google: forged signature", func() response {
			return h.googleSignIn(idp.sign(forged, "g1", map[string]any{
				"iss": "https://accounts.google.com", "aud": testGoogleAudience, "sub": "g-diag-3",
				"exp": now.Add(time.Hour).Unix(), "nonce": "google-nonce"}), "google-nonce")
		}, "bad_signature", map[string]string{"kid": "g1"}},
		{"google: unknown key id", func() response {
			return h.googleSignIn(idp.sign(idp.google, "g-rotated", map[string]any{
				"iss": "https://accounts.google.com", "aud": testGoogleAudience, "sub": "g-diag-4",
				"exp": now.Add(time.Hour).Unix(), "nonce": "google-nonce"}), "google-nonce")
		}, "unknown_key_id", map[string]string{"kid": "g-rotated"}},
		{"google: audience", func() response {
			return h.googleSignIn(idp.googleToken("g-diag-5", "g5@gmail.com", map[string]any{
				"aud": "999-other.apps.googleusercontent.com"}), "google-nonce")
		}, "audience_mismatch", map[string]string{"token_aud": "[999-other.apps.googleusercontent.com]",
			"configured_aud": "[123-ios.apps.googleusercontent.com " + testGoogleAudience + "]",
			"token_azp":      "123-android.apps.googleusercontent.com"}},
		{"google: garbage", func() response { return h.googleSignIn("not.a.token", "google-nonce") }, "malformed", nil},
	}
	for _, c := range cases {
		before := len(h.rejections())
		mustStatus(t, c.send(), http.StatusUnauthorized, "INVALID_ID_TOKEN")
		logged := h.rejections()
		if len(logged) != before+1 {
			t.Fatalf("%s: %d rejection lines, want exactly one", c.name, len(logged)-before)
		}
		entry := logged[len(logged)-1]
		if entry["level"] != "WARN" || entry["reason"] != c.reason {
			t.Fatalf("%s: logged %v, want reason %q", c.name, entry, c.reason)
		}
		for key, want := range c.fields {
			if got := fmt.Sprint(entry[key]); got != want {
				t.Fatalf("%s: %s = %q, want %q (%v)", c.name, key, got, want, entry)
			}
		}
	}
	logs := h.logs.String()
	for _, secret := range []string{"raw-nonce", appleNonce, "google-nonce", "@icloud.com", "@gmail.com", "unverified@example.com"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("the log contains %q", secret)
		}
	}
}
