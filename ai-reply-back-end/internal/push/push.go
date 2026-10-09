// Package push — провайдерге тәуелсіз хабарлама және оны жеткізуші (FCM).
//
// Business code never talks to Firebase directly: it creates a notification,
// the dispatcher turns each queued delivery into a Message and hands it to
// the Provider. Android and iOS both go through FCM HTTP v1; for iOS the same
// request carries an "apns" block and Firebase forwards it to APNs with the
// project's APNs key. A provider answers with a classified Result, so the
// dispatcher can tell "try again later" from "this token is dead" from "this
// message can never be sent" without knowing any provider status code.
//
// Only the standard library is used: a service-account JWT is exchanged for
// an OAuth access token, no legacy server keys.
package push

import (
	"context"
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
	// Category — domain category; the iOS thread and the Android channel follow it.
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
	// Rejected — this message can never be sent as is (payload, project,
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
	MessageID  string        // FCM message name
	Code       string        // provider error code, e.g. UNREGISTERED
	Detail     string        // short and safe: never a token, never a key
	StatusCode int           // HTTP status, 0 for transport errors
	RetryAfter time.Duration // provider hint, 0 when absent
}

// Provider — хабарлама жеткізушісі.
type Provider interface {
	// Name — "fcm".
	Name() string
	// Send — one message to one device. It never returns a raw error: every
	// failure is classified into the Result.
	Send(ctx context.Context, target Target, msg Message) Result
}

// Payload шектері: FCM HTTP v1 сұранысы 4000 байт, APNs-ке жететін бөлігі 4096 байт.
// Кезекке қоймай тұрып тексеріледі.
const (
	MaxFCMPayloadBytes  = 4000
	MaxAPNsPayloadBytes = 4096
)

// PayloadSizes — хабарламаның FCM сұранысындағы және APNs-ке жететін өлшемі
// (токеннің ең ұзын түрімен).
//
// The APNs part is what Firebase builds for the device: the "aps" dictionary
// with the alert, sound and thread, plus every data key at the top level.
func PayloadSizes(msg Message) (fcm, apns int) {
	request := buildFCMMessage(strings.Repeat("x", 256), msg, time.Now())
	f, _ := json.Marshal(request)
	device := map[string]any{}
	for k, v := range msg.Data {
		device[k] = v
	}
	aps := request.Message.APNs.Payload.APS
	device["aps"] = map[string]any{
		"alert": map[string]string{"title": msg.Title, "body": msg.Body},
		"sound": aps.Sound, "thread-id": aps.ThreadID,
	}
	a, _ := json.Marshal(device)
	return len(f), len(a)
}

// TokenHash — токеннің SHA-256 хэші (hex): бірегейлік пен журнал үшін.
func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------- helpers

var errKey = errors.New("push: private key is not a valid PEM key of the expected type")

// parseRSAKey — Google қызметтік тіркелгісінің кілті (PKCS#8, кейде PKCS#1).
func parseRSAKey(raw string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(raw)))
	if block == nil {
		return nil, errKey
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, errKey
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
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

// safeDetail — қысқа, бір жолды мәтін. FCM мекенжайында токен жоқ, токен тек денеде.
func safeDetail(s string) string {
	s = strings.Join(strings.Fields(s), " ")
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
