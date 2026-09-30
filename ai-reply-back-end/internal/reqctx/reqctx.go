// Package reqctx — сұраныс корреляциясы және клиент метадерегі (контекст арқылы).
//
// The mobile apps describe themselves in headers (X-Platform, X-App-Version,
// X-App-Build, X-OS-Version, X-Installation-ID, X-Session-ID, X-Request-ID).
// Those values are hints for logs, diagnostics and analytics only: they are
// sanitized here and nothing that decides access may ever read them —
// authentication stays with the bearer token and the admin session.
package reqctx

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Header names the apps send.
const (
	HeaderRequestID      = "X-Request-ID"
	HeaderPlatform       = "X-Platform"
	HeaderAppVersion     = "X-App-Version"
	HeaderAppBuild       = "X-App-Build"
	HeaderOSVersion      = "X-OS-Version"
	HeaderInstallationID = "X-Installation-ID"
	HeaderSessionID      = "X-Session-ID"
	HeaderTraceParent    = "traceparent"
)

// Client — sanitized metadata of the calling app.
type Client struct {
	RequestID      string
	TraceID        string
	Platform       string
	AppVersion     string
	AppBuild       string
	OSVersion      string
	InstallationID string
	SessionID      string
}

var (
	requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,64}$`)
	idPattern        = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)
	versionPattern   = regexp.MustCompile(`^[0-9A-Za-z.+_ ()-]{1,32}$`)
	traceParent      = regexp.MustCompile(`^[0-9a-f]{2}-([0-9a-f]{32})-[0-9a-f]{16}-[0-9a-f]{2}$`)
)

// Parse — тақырыптарды оқып, тазалайды. Жарамсыз мән жай ғана бос қалады.
func Parse(r *http.Request) Client {
	c := Client{
		RequestID:      RequestIDOrNew(r.Header.Get(HeaderRequestID)),
		Platform:       Platform(r.Header.Get(HeaderPlatform)),
		AppVersion:     Version(r.Header.Get(HeaderAppVersion)),
		AppBuild:       Version(r.Header.Get(HeaderAppBuild)),
		OSVersion:      Version(r.Header.Get(HeaderOSVersion)),
		InstallationID: ID(r.Header.Get(HeaderInstallationID)),
		SessionID:      ID(r.Header.Get(HeaderSessionID)),
	}
	if m := traceParent.FindStringSubmatch(strings.ToLower(strings.TrimSpace(r.Header.Get(HeaderTraceParent)))); m != nil &&
		m[1] != strings.Repeat("0", 32) {
		c.TraceID = m[1]
	}
	return c
}

// RequestIDOrNew — клиенттің идентификаторы жарамды болса сол, әйтпесе жаңасы.
//
// A client-chosen id makes a mobile error report findable in the server logs;
// the pattern keeps log lines free of injected text and bounded in size.
func RequestIDOrNew(raw string) string {
	raw = strings.TrimSpace(raw)
	if requestIDPattern.MatchString(raw) {
		return raw
	}
	return "req_" + traits.RandomToken(8)
}

// Platform — ios | android | web, әйтпесе бос.
func Platform(raw string) string {
	switch v := strings.ToLower(strings.TrimSpace(raw)); v {
	case "ios", "android", "web":
		return v
	default:
		return ""
	}
}

// Version — нұсқа не build нөмірі (қысқа, қауіпсіз таңбалар).
func Version(raw string) string {
	raw = strings.TrimSpace(raw)
	if versionPattern.MatchString(raw) {
		return raw
	}
	return ""
}

// ID — қосымша жасаған идентификатор (орнату, сессия).
func ID(raw string) string {
	raw = strings.TrimSpace(raw)
	if idPattern.MatchString(raw) {
		return raw
	}
	return ""
}

type ctxKey int

const (
	clientKey ctxKey = iota
	observedKey
)

// With — метадеректі контекстке салады.
func With(ctx context.Context, c Client) context.Context {
	return context.WithValue(ctx, clientKey, c)
}

// From — контекстегі метадерек (жоқ болса бос).
func From(ctx context.Context) Client {
	c, _ := ctx.Value(clientKey).(Client)
	return c
}

// Observed — өңдеуші сұраныс кезінде толтыратын мәндер (кіру журналы үшін).
//
// The access log wraps the router, so it cannot see what a handler learned.
// requireUser writes the authenticated user here; the log line and the API
// error record read it afterwards. (The error code travels through the
// response writer instead, see httpx.Error.)
type Observed struct {
	mu     sync.Mutex
	userID string
}

// SetUser — аутентификацияланған қолданушы.
func (o *Observed) SetUser(id string) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.userID = id
	o.mu.Unlock()
}

// User — белгілі болса, қолданушы идентификаторы.
func (o *Observed) User() string {
	if o == nil {
		return ""
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.userID
}

// WithObserved — толтырылатын жинақты контекстке салады.
func WithObserved(ctx context.Context, o *Observed) context.Context {
	return context.WithValue(ctx, observedKey, o)
}

// ObservedFrom — жинақ (жоқ болса nil; nil-мен барлық әдіс қауіпсіз).
func ObservedFrom(ctx context.Context) *Observed {
	o, _ := ctx.Value(observedKey).(*Observed)
	return o
}
