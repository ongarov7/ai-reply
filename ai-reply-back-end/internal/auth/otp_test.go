package auth

import (
	"bytes"
	"crypto/rand"
	"errors"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

func TestNewOTPCodeIsFourDigitsWithLeadingZeros(t *testing.T) {
	// crypto/rand.Int reads two bytes for 0..9999 and masks the top two bits.
	for _, tc := range []struct {
		entropy []byte
		want    string
	}{
		{[]byte{0x00, 0x00}, "0000"},
		{[]byte{0x00, 0x2a}, "0042"},
		{[]byte{0x01, 0x80}, "0384"},
		{[]byte{0x27, 0x0f}, "9999"},
		// 16383 is out of range: rejection sampling reads again, no modulo bias.
		{[]byte{0xff, 0xff, 0x00, 0x07}, "0007"},
	} {
		got, err := NewOTPCode(bytes.NewReader(tc.entropy))
		if err != nil || got != tc.want {
			t.Fatalf("NewOTPCode(%x) = %q, %v; want %q", tc.entropy, got, err, tc.want)
		}
	}
	if _, err := NewOTPCode(bytes.NewReader(nil)); err == nil {
		t.Fatal("an exhausted entropy source must be an error, not a weak code")
	}
}

func TestNewOTPCodeFromCryptoRand(t *testing.T) {
	seen := map[string]bool{}
	leadingZero := false
	for i := 0; i < 3000; i++ {
		code, err := NewOTPCode(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if !isOTPCode(code) {
			t.Fatalf("malformed code %q", code)
		}
		leadingZero = leadingZero || code[0] == '0'
		seen[code] = true
	}
	// 3000 draws from 10000 values: a fixed or badly biased generator fails.
	if len(seen) < 2000 || !leadingZero {
		t.Fatalf("distinct=%d leadingZero=%v", len(seen), leadingZero)
	}
}

func TestIsOTPCode(t *testing.T) {
	for code, want := range map[string]bool{
		"0384": true, "9999": true, "384": false, "03845": false, "03a4": false,
		" 038": false, "": false, "١٢٣٤": false, "12.4": false,
	} {
		if got := isOTPCode(code); got != want {
			t.Errorf("isOTPCode(%q) = %v", code, got)
		}
	}
}

func TestHashEmailOTPBindsPurposeAddressAndKey(t *testing.T) {
	key := deriveKey("test-access-secret-that-is-long-enough-000", "ai-reply/email-otp/v1")
	base := hashEmailOTP(key, domain.OTPPurposeLogin, "user@example.com", "0384")
	if base != hashEmailOTP(key, domain.OTPPurposeLogin, "user@example.com", "0384") {
		t.Fatal("hash must be deterministic")
	}
	if !strings.HasPrefix(base, "h1:") || strings.Contains(base, "0384") {
		t.Fatalf("hash %q", base)
	}
	others := []string{
		hashEmailOTP(key, domain.OTPPurposeLinkEmail, "user@example.com", "0384"),
		hashEmailOTP(key, domain.OTPPurposeLogin, "other@example.com", "0384"),
		hashEmailOTP(key, domain.OTPPurposeLogin, "user@example.com", "0385"),
		hashEmailOTP(deriveKey("another-secret-that-is-long-enough-000000", "ai-reply/email-otp/v1"),
			domain.OTPPurposeLogin, "user@example.com", "0384"),
	}
	for _, other := range others {
		if other == base {
			t.Fatal("purpose, address, code and key must all change the hash")
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	valid := map[string]string{
		"user@example.com":              "user@example.com",
		"  User.Name+tag@Example.KZ ":   "user.name+tag@example.kz",
		"AIGERIM@MAIL.RU":               "aigerim@mail.ru",
		"a@b.co":                        "a@b.co",
		"x7k2@privaterelay.appleid.com": "x7k2@privaterelay.appleid.com",
	}
	for raw, want := range valid {
		got, err := NormalizeEmail(raw)
		if err != nil || got != want {
			t.Errorf("NormalizeEmail(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{
		"", "   ", "plainaddress", "@example.com", "user@", "user@localhost", "user@.example.com",
		"user@example..com", "user@example.com.", "User <user@example.com>", "user@example.com (work)",
		"us er@example.com", "user@exa mple.com", "user\n@example.com", "a@b@c.com",
		strings.Repeat("a", 65) + "@example.com", "user@" + strings.Repeat("d", 250) + ".com",
	} {
		if got, err := NormalizeEmail(raw); !errors.Is(err, domain.ErrInvalidEmail) {
			t.Errorf("NormalizeEmail(%q) = %q, %v; want ErrInvalidEmail", raw, got, err)
		}
	}
}

func TestParseIdentityUsesTheSameEmailRules(t *testing.T) {
	id, err := ParseIdentity(" Someone@Example.com ")
	if err != nil || id.Kind != domain.IdentityEmail || id.Value != "someone@example.com" {
		t.Fatalf("ParseIdentity = %+v, %v", id, err)
	}
	if _, err := ParseIdentity("Someone <someone@example.com>"); err == nil {
		t.Fatal("display-name forms are rejected everywhere, not only on the new endpoint")
	}
}

func TestProviderEmailAuthority(t *testing.T) {
	google := []struct {
		address, hd string
		want        bool
	}{
		{"someone@gmail.com", "", true},
		{"someone@googlemail.com", "", true},
		{"ceo@acme.kz", "acme.kz", true},
		{"ceo@acme.kz", "ACME.KZ", true},
		{"ceo@acme.kz", "", false},
		{"ceo@acme.kz", "other.kz", false},
		{"someone@gmail.com.evil.kz", "", false},
	}
	for _, tc := range google {
		if got := googleAuthoritative(tc.address, tc.hd); got != tc.want {
			t.Errorf("googleAuthoritative(%q, %q) = %v", tc.address, tc.hd, got)
		}
	}
	apple := []struct {
		address string
		private bool
		want    bool
	}{
		{"x7k2@privaterelay.appleid.com", true, true},
		{"x7k2@privaterelay.appleid.com", false, false},
		{"someone@icloud.com", false, true},
		{"someone@me.com", false, true},
		{"someone@mac.com", false, true},
		{"someone@gmail.com", false, false},
		{"someone@gmail.com", true, false},
	}
	for _, tc := range apple {
		if got := appleAuthoritative(tc.address, tc.private); got != tc.want {
			t.Errorf("appleAuthoritative(%q, %v) = %v", tc.address, tc.private, got)
		}
	}
}

func TestCleanDisplayName(t *testing.T) {
	for raw, want := range map[string]string{
		"  Айгерім   Сейітқызы ":   "Айгерім Сейітқызы",
		"Name\u0000With\nControls": "Name With Controls",
		"":                         "",
	} {
		if got := cleanDisplayName(raw); got != want {
			t.Errorf("cleanDisplayName(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := cleanDisplayName(strings.Repeat("я", 200)); len([]rune(got)) != 80 {
		t.Fatalf("display name must be clamped to 80 runes, got %d", len([]rune(got)))
	}
}
