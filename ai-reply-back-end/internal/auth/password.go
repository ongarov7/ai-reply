package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"crypto/pbkdf2"
)

// Ашық құпиясөз ешқашан сақталмайды: PBKDF2-HMAC-SHA256.
const (
	pbkdf2Iterations = 600_000 // OWASP ұсынысы (SHA-256)
	pbkdf2KeyLength  = 32
	pbkdf2SaltLength = 16
)

// ErrPasswordMismatch — құпиясөз сәйкес емес.
var ErrPasswordMismatch = errors.New("auth: password mismatch")

// dummyHash — ешбір құпиясөзге сәйкес келмейтін, бірақ нақты хэшпен бірдей
// бағалы хэш (сол итерация саны, сол ұзындықтар).
var dummyHash = fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iterations,
	base64.RawStdEncoding.EncodeToString(make([]byte, pbkdf2SaltLength)),
	base64.RawStdEncoding.EncodeToString(make([]byte, pbkdf2KeyLength)))

// VerifyDummyPassword — белгісіз тіркелгіге де дәл сондай уақыт жұмсайды.
//
// Answering an unknown e-mail at once and a known one after a full PBKDF2 run
// would tell an attacker which addresses are accounts. It always fails.
func VerifyDummyPassword(password string) {
	_ = VerifyPassword(dummyHash, password)
}

// HashPassword — жаңа хэш: pbkdf2-sha256$iter$salt$key.
func HashPassword(password string) (string, error) {
	salt := make([]byte, pbkdf2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, pbkdf2KeyLength)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword — тұрақты уақытта салыстыру.
func VerifyPassword(hash, password string) error {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return ErrPasswordMismatch
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return ErrPasswordMismatch
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return ErrPasswordMismatch
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return ErrPasswordMismatch
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(expected))
	if err != nil {
		return ErrPasswordMismatch
	}
	if subtle.ConstantTimeCompare(actual, expected) != 1 {
		return ErrPasswordMismatch
	}
	return nil
}
