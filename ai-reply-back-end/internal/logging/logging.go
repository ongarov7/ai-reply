// Package logging — құрылымдық журнал. Мәтін, токен, құпия ешқашан жазылмайды.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/redact"
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

// Тыйым салынған кілттер: мұндай өріс журналға түссе, мәні алмастырылады.
//
// Content keys are listed here (a message, a reply, a prompt never reaches a
// log line); secrets — passwords, codes, tokens, keys, authorization, cookies
// — are decided by redact.IsSensitiveKey, shared with every stored record.
var forbidden = map[string]bool{
	"message": true, "source_text": true, "instruction": true, "reply": true,
	"prompt": true, "text": true, "body": true, "content": true,
	"phone": true, "email": true,
}

// New — журнал құрады (json немесе мәтін).
func New(level, format string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lv, ReplaceAttr: scrub}
	var handler slog.Handler
	if strings.ToLower(format) == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

// scrub — соңғы қорғаныс шебі: құпия өріс кездейсоқ берілсе де жазылмайды.
func scrub(_ []string, a slog.Attr) slog.Attr {
	if forbidden[strings.ToLower(a.Key)] || redact.IsSensitiveKey(a.Key) {
		return slog.String(a.Key, redact.Placeholder)
	}
	return a
}

// WithRequestID — сұраныс идентификаторын контекстке салады.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID — контекстен идентификатор.
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// FromContext — сұраныс идентификаторы қосылған журнал.
func FromContext(ctx context.Context, base *slog.Logger) *slog.Logger {
	if id := RequestID(ctx); id != "" {
		return base.With("request_id", id)
	}
	return base
}
