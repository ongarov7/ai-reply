// Package redact — құпия мәндерді жасыру және жеке деректерді бүркемелеу.
//
// One place decides what may never be written anywhere (passwords, one-time
// codes, tokens, provider keys, authorization headers, cookies, payment
// credentials) and how personal data is shortened when an exact value is not
// needed (e-mail, phone, IP). Loggers, stored events and error records all go
// through these helpers instead of every call site remembering the rules.
package redact

import (
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"unicode"
)

// Placeholder — жасырылған мәннің орнына жазылатын белгі.
const Placeholder = "[redacted]"

// sensitiveKeys — мәні ешқашан журналға не жазбаға түспейтін кілттер.
var sensitiveKeys = map[string]bool{
	"password": true, "password_confirmation": true, "new_password": true, "old_password": true,
	"otp": true, "otp_code": true, "verification_code": true, "one_time_code": true,
	"authorization": true, "proxy-authorization": true, "cookie": true, "set-cookie": true,
	"token": true, "access_token": true, "refresh_token": true, "id_token": true,
	"identity_token": true, "push_token": true, "device_token": true, "registration_token": true,
	"api_key": true, "apikey": true, "openai_api_key": true, "resend_api_key": true,
	"secret": true, "client_secret": true, "private_key": true, "firebase_private_key": true,
	"apns_private_key": true, "credentials": true, "google_credentials": true, "apple_credentials": true,
	"jwt": true, "csrf": true, "x-csrf-token": true, "x-api-key": true,
	"card": true, "card_number": true, "pan": true, "cvv": true, "cvc": true, "iban": true,
}

// sensitiveSuffixes — осылармен аяқталатын кілттер де құпия ("input_tokens" емес).
var sensitiveSuffixes = []string{
	"_token", "_secret", "_password", "_private_key", "_api_key", "_credentials", "_otp",
}

// IsSensitiveKey — кілттің мәнін жасыру керек пе.
func IsSensitiveKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if sensitiveKeys[k] {
		return true
	}
	for _, suffix := range sensitiveSuffixes {
		if strings.HasSuffix(k, suffix) {
			return true
		}
	}
	return false
}

// Map — құпия кілттердің мәнін ауыстырған көшірме (ішкі map-тер де тексеріледі).
func Map(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if IsSensitiveKey(k) {
			out[k] = Placeholder
			continue
		}
		switch inner := v.(type) {
		case map[string]any:
			out[k] = Map(inner)
		case map[string]string:
			copied := make(map[string]any, len(inner))
			for ik, iv := range inner {
				copied[ik] = iv
			}
			out[k] = Map(copied)
		default:
			out[k] = v
		}
	}
	return out
}

// Headers — тақырыптардың журналға жарамды көшірмесі: Authorization, Cookie т.б. жасырылады.
func Headers(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if IsSensitiveKey(k) || strings.EqualFold(k, "X-Installation-Key") {
			out[k] = Placeholder
			continue
		}
		out[k] = strings.Join(h.Values(k), ", ")
	}
	return out
}

// Email — "yerek@example.com" → "y***@example.com".
func Email(v string) string {
	v = strings.TrimSpace(v)
	at := strings.LastIndexByte(v, '@')
	if at <= 0 {
		return strings.Repeat("*", len([]rune(v)))
	}
	name := []rune(v[:at])
	return string(name[:1]) + "***" + v[at:]
}

// Phone — "+77011234567" → "+7******4567" (соңғы 4 цифр қалады).
func Phone(v string) string {
	r := []rune(strings.TrimSpace(v))
	if len(r) <= 4 {
		return strings.Repeat("*", len(r))
	}
	prefix := 0
	if r[0] == '+' {
		prefix = 2
		if len(r) < 8 {
			prefix = 1
		}
	}
	masked := make([]rune, 0, len(r))
	masked = append(masked, r[:prefix]...)
	for i := prefix; i < len(r)-4; i++ {
		masked = append(masked, '*')
	}
	return string(append(masked, r[len(r)-4:]...))
}

// Identifier — пошта не телефон, түріне қарай.
func Identifier(v string) string {
	if strings.Contains(v, "@") {
		return Email(v)
	}
	return Phone(v)
}

// IP — соңғы бөлігі жасырылған мекенжай: 203.0.113.x, 2001:db8:1:2::x.
func IP(v string) string {
	ip := net.ParseIP(strings.TrimSpace(v))
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.x", v4[0], v4[1], v4[2])
	}
	masked := make(net.IP, len(ip))
	copy(masked, ip)
	for i := 8; i < len(masked); i++ {
		masked[i] = 0
	}
	return strings.TrimSuffix(masked.String(), "::") + "::x"
}

// Text — бір жолды, басқару таңбаларынсыз, max таңбаға дейін қысқартылған мәтін.
//
// Used for provider error details and other free text that may reach a log
// or an admin screen: newlines cannot forge log lines and nothing unbounded
// is stored.
func Text(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 {
		if r := []rune(s); len(r) > max {
			s = strings.TrimSpace(string(r[:max]))
		}
	}
	return s
}
