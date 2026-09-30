// Package push — провайдерге тәуелсіз хабарлама және оны жеткізушілер (FCM, APNs).
//
// Business code never talks to Firebase or Apple directly: it creates a
// notification, the dispatcher turns each queued delivery into a Message and
// hands it to the Provider of the installation's platform. A provider answers
// with a classified Result, so the dispatcher can tell "try again later" from
// "this token is dead" from "this message can never be sent" without knowing
// any provider-specific status code.
//
// Both providers use only the standard library: FCM HTTP v1 with a
// service-account JWT exchanged for an OAuth access token, and APNs over
// HTTP/2 with a token-based (.p8) provider JWT. No legacy FCM server keys, no
// APNs certificates.
package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Message — провайдерге тәуелсіз хабарлама.
type Message struct {
	Title string
	Body  string
	// Data — the flat string payload the app reads: nid, did, type, category,
	// link and any custom keys. Both platforms receive the same keys.
	Data map[string]string
	// Category — domain category; decides priority and the Android channel.
	Category string
	// CollapseID — the notification id. APNs collapses and Android replaces a
	// notification with the same id, so a delivery repeated after an unknown
	// outcome shows up once on the device instead of twice.
	CollapseID string
	// TTL — how long the provider may keep an undelivered message.
	TTL time.Duration
	// Important — high priority and the "important" Android channel.
	Important bool
}

// Target — бір құрылғы.
type Target struct {
	Token string
	// Environment — APNs only: sandbox or production. Empty means the
	// provider's configured default.
	Environment string
}

// Outcome — жіберу нәтижесінің түрі.
type Outcome int

const (
	// Accepted — the provider took the message. It does not mean a person saw it.
	Accepted Outcome = iota + 1
	// Retry — a transient failure (timeout, 429, 5xx): try again later.
	Retry
	// InvalidToken — the token is dead for good: stop using it.
	InvalidToken
	// Rejected — this message can never be sent as is (payload, topic,
	// credentials): do not retry, do not blame the token.
	Rejected
)

// String — журнал үшін.
func (o Outcome) String() string {
	switch o {
	case Accepted:
		return "accepted"
	case Retry:
		return "retry"
	case InvalidToken:
		return "invalid_token"
	case Rejected:
		return "rejected"
	default:
		return "unknown"
	}
}

// Result — провайдердің жіктелген жауабы.
type Result struct {
	Outcome    Outcome
	MessageID  string        // FCM message name / apns-id
	Code       string        // provider error code, e.g. UNREGISTERED, BadDeviceToken
	Detail     string        // short and safe: never a token, never a key
	StatusCode int           // HTTP status, 0 for transport errors
	RetryAfter time.Duration // provider hint, 0 when absent
}

// Provider — бір платформаның жеткізушісі.
type Provider interface {
	// Name — "fcm" or "apns".
	Name() string
	// Send — one message to one device. It never returns a raw error: every
	// failure is classified into the Result.
	Send(ctx context.Context, target Target, msg Message) Result
}

// Payload шектері: APNs 4096 байт, FCM 4000 байт. Кезекке қоймай тұрып тексеріледі.
const (
	MaxAPNsPayloadBytes = 4096
	MaxFCMPayloadBytes  = 4000
)

// PayloadSizes — хабарламаның әр провайдердегі өлшемі (токеннің ең ұзын түрімен).
func PayloadSizes(msg Message) (fcm, apns int) {
	f, _ := json.Marshal(buildFCMMessage(strings.Repeat("x", 256), msg))
	a, _ := json.Marshal(BuildAPNsPayload(msg))
	return len(f), len(a)
}

// TokenHash — токеннің SHA-256 хэші (hex): бірегейлік пен журнал үшін.
func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------- helpers

var errKey = errors.New("push: private key is not a valid PEM key of the expected type")

func parsePEMBlock(raw string) ([]byte, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(raw)))
	if block == nil {
		return nil, errKey
	}
	return block.Bytes, nil
}

// parseRSAKey — Google қызметтік тіркелгісінің кілті (PKCS#8, кейде PKCS#1).
func parseRSAKey(raw string) (*rsa.PrivateKey, error) {
	der, err := parsePEMBlock(raw)
	if err != nil {
		return nil, err
	}
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, errKey
	}
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	return nil, errKey
}

// parseECKey — Apple .p8 кілті (PKCS#8, P-256).
func parseECKey(raw string) (*ecdsa.PrivateKey, error) {
	der, err := parsePEMBlock(raw)
	if err != nil {
		return nil, err
	}
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if ecKey, ok := key.(*ecdsa.PrivateKey); ok && ecKey.Curve.Params().BitSize == 256 {
			return ecKey, nil
		}
		return nil, errKey
	}
	if key, err := x509.ParseECPrivateKey(der); err == nil && key.Curve.Params().BitSize == 256 {
		return key, nil
	}
	return nil, errKey
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func jwtSegment(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return b64url(raw), nil
}

// retryAfter — "Retry-After" тақырыбы (секунд не HTTP күні).
func retryAfter(header string, now time.Time) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if at, err := http.ParseTime(header); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

// transportFailure — желі қатесі: нәтиже белгісіз, қайталау керек.
func transportFailure(ctx context.Context, err error) Result {
	code := "NETWORK_ERROR"
	if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
		code = "TIMEOUT"
	}
	return Result{Outcome: Retry, Code: code, Detail: safeDetail(err.Error())}
}

// safeDetail — қысқа, бір жолды мәтін (желі қатесінде URL ішіндегі токен болмауы үшін қиылады).
func safeDetail(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	// Transport errors quote the request URL, and an APNs URL ends with the
	// device token: cut everything from the path on.
	if i := strings.Index(s, "/3/device/"); i >= 0 {
		s = s[:i] + "/3/device/…"
	}
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160])
	}
	return s
}

func statusDetail(status int, code string) string {
	if code == "" {
		return fmt.Sprintf("HTTP %d", status)
	}
	return fmt.Sprintf("HTTP %d %s", status, code)
}
