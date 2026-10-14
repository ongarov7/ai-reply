package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Дауыс қателері: шек — 429 VOICE_LIMIT_REACHED, жазба — 400 INVALID_REQUEST және details.field = audio.
func TestFailCarriesVoiceErrorDetails(t *testing.T) {
	resets := time.Date(2026, 3, 11, 0, 0, 0, 0, time.FixedZone("ALMT", 5*3600))
	for _, c := range []struct {
		err     error
		status  int
		code    string
		details map[string]any
	}{
		{&domain.VoiceLimitError{Limit: 100, ResetsAt: resets}, http.StatusTooManyRequests, CodeVoiceLimit,
			map[string]any{"daily_limit": float64(100), "resets_at": "2026-03-11T00:00:00+05:00"}},
		{&domain.AudioError{Reason: "too_long", MaxSeconds: 40, MaxBytes: 4 << 20}, http.StatusBadRequest, CodeInvalidRequest,
			map[string]any{"field": "audio", "reason": "too_long", "max_seconds": float64(40), "max_bytes": float64(4 << 20)}},
	} {
		w := httptest.NewRecorder()
		Fail(w, c.err)
		var body errorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != c.status || body.Error.Code != c.code || !reflect.DeepEqual(body.Error.Details, c.details) {
			t.Errorf("%v: %d %+v", c.err, w.Code, body.Error)
		}
		if w.Header().Get("Retry-After") != "" {
			t.Errorf("%v: no Retry-After for a daily cap", c.err)
		}
	}
}

// TRUST_PROXY: X-Forwarded-For-тің оң жақтағы (прокси қосқан) мәні; X-Real-IP оқылмайды.
func TestClientIPUsesTheProxyAppendedAddress(t *testing.T) {
	for _, tc := range []struct {
		name       string
		forwarded  []string
		realIP     string
		trustProxy bool
		want       string
	}{
		{"no proxy header", nil, "", true, "192.0.2.10"},
		{"single entry", []string{"203.0.113.7"}, "", true, "203.0.113.7"},
		{"forged left entry", []string{"1.2.3.4, 203.0.113.7"}, "", true, "203.0.113.7"},
		{"spaces", []string{" 1.2.3.4 ,  203.0.113.7  "}, "", true, "203.0.113.7"},
		{"repeated header", []string{"1.2.3.4", "5.6.7.8, 203.0.113.7"}, "", true, "203.0.113.7"},
		{"ipv6", []string{"1.2.3.4, 2001:db8::1"}, "", true, "2001:db8::1"},
		{"real ip ignored", nil, "1.2.3.4", true, "192.0.2.10"},
		{"garbage falls back", []string{"1.2.3.4, not-an-ip"}, "", true, "192.0.2.10"},
		{"empty last entry", []string{"1.2.3.4, "}, "", true, "192.0.2.10"},
		{"proxy not trusted", []string{"203.0.113.7"}, "1.2.3.4", false, "192.0.2.10"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.0.2.10:51234"
		for _, v := range tc.forwarded {
			req.Header.Add("X-Forwarded-For", v)
		}
		if tc.realIP != "" {
			req.Header.Set("X-Real-IP", tc.realIP)
		}
		if got := ClientIP(req, tc.trustProxy); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
