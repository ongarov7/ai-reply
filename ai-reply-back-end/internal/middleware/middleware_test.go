package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/logging"
)

// X-Request-ID тек қысқа [A-Za-z0-9-] болса қабылданады; әйтпесе жаңасы жасалады.
func TestRequestIDAcceptsOnlySafeValues(t *testing.T) {
	var seen string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = logging.RequestID(r.Context())
	}))
	for raw, keep := range map[string]bool{
		"3f2a-9b1c":             true,
		strings.Repeat("a", 64): true,
		strings.Repeat("a", 65): false,
		"abc\nforged=1":         false,
		"<script>":              false,
		"id with spaces":        false,
		"":                      false,
		"9b1c_3f2a":             false,
	} {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		if raw != "" {
			req.Header["X-Request-Id"] = []string{raw}
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		got := rec.Header().Get("X-Request-ID")
		if got != seen || got == "" {
			t.Fatalf("%q: header %q, context %q", raw, got, seen)
		}
		if keep != (got == raw) {
			t.Errorf("%q: kept = %v, want %v (got %q)", raw, got == raw, keep, got)
		}
		if !requestIDPattern.MatchString(got) && !keep {
			t.Errorf("%q: replacement %q is not a safe id", raw, got)
		}
	}
}

// HSTS тек сұралғанда (production не https мекенжайы) жіберіледі.
func TestSecurityHeadersHSTS(t *testing.T) {
	for _, hsts := range []bool{true, false} {
		rec := httptest.NewRecorder()
		SecurityHeaders(hsts)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
			ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if got := rec.Header().Get("Strict-Transport-Security"); (got != "") != hsts {
			t.Errorf("hsts %v: header %q", hsts, got)
		}
	}
}
