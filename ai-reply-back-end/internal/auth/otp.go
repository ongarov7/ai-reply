package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/phone"
)

// Код пішімі: дәл 4 ондық таңба, бастапқы нөлдер сақталады (0384).
const (
	otpDigits = 4
	otpSpace  = 10000
)

// Identity — нормаланған кіру идентификаторы.
type Identity struct {
	Kind    string // phone | email
	Value   string
	Country string
	Masked  string
}

// ParseIdentity — телефон (E.164, 12 ел) немесе email.
func ParseIdentity(raw string) (Identity, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Identity{}, phone.ErrEmpty
	}
	if strings.Contains(trimmed, "@") {
		value, err := NormalizeEmail(trimmed)
		if err != nil {
			return Identity{}, err
		}
		return Identity{Kind: domain.IdentityEmail, Value: value, Masked: maskEmail(value)}, nil
	}
	num, err := phone.Parse(trimmed)
	if err != nil {
		return Identity{}, err
	}
	return Identity{Kind: domain.IdentityPhone, Value: num.E164, Country: num.Country, Masked: maskPhone(num.E164)}, nil
}

// NewOTPCode — криптографиялық кездейсоқ 4 таңбалы код (0000–9999, біркелкі үлестірім).
//
// crypto/rand only: math/rand is predictable, and a predictable four-digit
// code is no code at all. The reader is a parameter so tests can pin it.
func NewOTPCode(random io.Reader) (string, error) {
	n, err := rand.Int(random, big.NewInt(otpSpace))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", otpDigits, n.Int64()), nil
}

// isOTPCode — дәл 4 ASCII цифр.
func isOTPCode(code string) bool {
	if len(code) != otpDigits {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}

// HashCode — телефон кодын хэштейді (ашық түрде ешқашан сақталмайды).
func HashCode(secret, kind, value, code string) string {
	sum := sha256.Sum256([]byte(secret + "|" + kind + "|" + value + "|" + code))
	return base64.RawStdEncoding.EncodeToString(sum[:])
}

// hashEmailOTP — поштаға жіберілген кодтың HMAC-SHA256 белгісі.
//
// The purpose and the address are part of the MAC, so a code issued to link
// an e-mail can never sign someone in, and a stored hash is useless for any
// other mailbox. The "h1:" prefix versions the format.
func hashEmailOTP(key []byte, purpose, address, code string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("email-otp|" + purpose + "|" + address + "|" + code))
	return "h1:" + base64.RawStdEncoding.EncodeToString(mac.Sum(nil))
}

// deriveKey — бір құпиядан мақсатына бөлек кілт туындатады (HMAC-SHA256).
func deriveKey(secret, label string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(label))
	return mac.Sum(nil)
}

// CompareCode — тұрақты уақытта салыстыру.
func CompareCode(expectedHash, actualHash string) bool {
	return subtle.ConstantTimeCompare([]byte(expectedHash), []byte(actualHash)) == 1
}

// LegacyClientID — ескі install_id-ден тұрақты, кері қайтарылмайтын идентификатор.
func LegacyClientID(secret, installID string) string {
	sum := sha256.Sum256([]byte(secret + "|install:" + installID))
	return hex.EncodeToString(sum[:])[:32]
}

func maskPhone(v string) string {
	if len(v) < 6 {
		return "***"
	}
	return v[:len(v)-6] + "***" + v[len(v)-2:]
}

func maskEmail(v string) string {
	at := strings.IndexByte(v, '@')
	if at <= 0 {
		return "***"
	}
	name := v[:at]
	if len(name) <= 2 {
		return "*" + v[at:]
	}
	return name[:1] + strings.Repeat("*", len(name)-2) + name[len(name)-1:] + v[at:]
}
