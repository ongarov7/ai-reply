package redact

import (
	"net/http"
	"testing"
)

func TestSensitiveKeys(t *testing.T) {
	for _, key := range []string{
		"password", "Password_Confirmation", "otp", "otp_code", "Authorization", "Cookie",
		"access_token", "refresh_token", "id_token", "identity_token", "push_token",
		"firebase_private_key", "APNS_PRIVATE_KEY", "google_credentials", "apple_credentials",
		"card_number", "cvv", "x-csrf-token", "stripe_api_key", "session_secret",
	} {
		if !IsSensitiveKey(key) {
			t.Errorf("%q must be redacted", key)
		}
	}
	for _, key := range []string{"input_tokens", "output_tokens", "platform", "error_code", "token_fingerprint", "user_id"} {
		if IsSensitiveKey(key) {
			t.Errorf("%q must stay readable", key)
		}
	}
}

func TestMapRedactsNested(t *testing.T) {
	out := Map(map[string]any{
		"user_id":  "u1",
		"password": "hunter2",
		"nested":   map[string]any{"refresh_token": "r", "ok": 1},
		"flat":     map[string]string{"otp": "1111", "name": "x"},
	})
	if out["password"] != Placeholder || out["user_id"] != "u1" {
		t.Fatalf("top level: %v", out)
	}
	nested := out["nested"].(map[string]any)
	if nested["refresh_token"] != Placeholder || nested["ok"] != 1 {
		t.Fatalf("nested: %v", nested)
	}
	flat := out["flat"].(map[string]any)
	if flat["otp"] != Placeholder || flat["name"] != "x" {
		t.Fatalf("flat: %v", flat)
	}
}

func TestHeadersRedactAuthorization(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer secret-token")
	h.Set("Cookie", "aireply_admin=abc")
	h.Set("X-CSRF-Token", "csrf")
	h.Set("X-App-Version", "1.3.2")
	out := Headers(h)
	for _, k := range []string{"Authorization", "Cookie", "X-Csrf-Token"} {
		if out[k] != Placeholder {
			t.Fatalf("%s = %q", k, out[k])
		}
	}
	if out["X-App-Version"] != "1.3.2" {
		t.Fatalf("version header lost: %v", out)
	}
}

func TestMasking(t *testing.T) {
	cases := map[string]string{
		Email("yerek@example.com"): "y***@example.com",
		Phone("+77011234567"):      "+7******4567",
		IP("203.0.113.77"):         "203.0.113.x",
		IP("2001:db8:1:2:3:4:5:6"): "2001:db8:1:2::x",
		IP("not-an-ip"):            "",
		Identifier("a@b.kz"):       "a***@b.kz",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func TestTextIsSingleLineAndBounded(t *testing.T) {
	got := Text("line one\nline\ttwo\r\n  three", 14)
	if got != "line one line" {
		t.Fatalf("got %q", got)
	}
	if len([]rune(Text("ааааааааааааааааааа", 5))) != 5 {
		t.Fatal("not truncated by runes")
	}
}
