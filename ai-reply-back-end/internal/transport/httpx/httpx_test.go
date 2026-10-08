package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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
