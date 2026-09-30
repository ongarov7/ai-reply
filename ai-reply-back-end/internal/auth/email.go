package auth

import (
	"net/mail"
	"strings"
	"unicode"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// maxEmailLength — RFC 5321 бойынша мекенжайдың ең үлкен ұзындығы.
const maxEmailLength = 254

// NormalizeEmail — поштаны бір ережемен нормалайды: шеткі бос орынсыз, кіші әріппен.
//
// Every lookup, code, sign-in and link goes through this one function, so the
// same person can never end up with two identities that differ only in case
// or padding. The whole address is lowercased: mailbox providers that treat
// the local part case-sensitively are vanishingly rare, while a user typing
// "Aigerim@" once and "aigerim@" the next time is not. Dots and +tags are
// kept — they are real, distinct addresses. Display-name forms such as
// "Name <a@b.kz>" are rejected rather than silently rewritten.
func NormalizeEmail(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" || len(value) > maxEmailLength {
		return "", domain.ErrInvalidEmail
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", domain.ErrInvalidEmail
		}
	}
	addr, err := mail.ParseAddress(value)
	if err != nil || addr.Name != "" || addr.Address != value {
		return "", domain.ErrInvalidEmail
	}
	at := strings.LastIndexByte(value, '@')
	local, host := value[:at], value[at+1:]
	if local == "" || len(local) > 64 || !strings.Contains(host, ".") ||
		strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return "", domain.ErrInvalidEmail
	}
	return value, nil
}
