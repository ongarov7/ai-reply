package apptest

import (
	"net/http"
	"strings"
	"testing"
)

// Демо режим өшірулі және SMS провайдері жоқ: телефонға «код жіберілді» деп айтылмайды.
func TestPhoneSignInIsRefusedWithoutDemoMode(t *testing.T) {
	h := newHarness(t, withEnv("AUTH_DEMO_MODE", "false"))
	res := h.do(http.MethodPost, "/api/v1/auth/request-otp",
		map[string]any{"identifier": "+7 701 404 40 40", "locale": "ru"}, nil)
	mustStatus(t, res, http.StatusServiceUnavailable, "AUTH_PROVIDER_UNAVAILABLE")
	if n := h.scalar(`SELECT COUNT(*) FROM otp_codes WHERE identity_kind = 'phone'`); n != 0 {
		t.Fatalf("%d phone codes stored", n)
	}
	if strings.Contains(h.logs.String(), `"msg":"otp issued"`) {
		t.Fatal("the stub pretended to send an SMS")
	}
}

// PAYMENT_MODE=off (әдепкі) не live (нақты провайдерсіз): әкімші ауыстырғышы да ештеңе сатпайды.
func TestPaymentModesWithoutAProviderSellNothing(t *testing.T) {
	for _, mode := range []string{"off", "live"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t, withEnv("PAYMENT_MODE", mode), withEnv("PAYMENT_DEMO_CHECKOUT", "false"))
			s := h.signIn("payments-" + mode + "@example.com")
			config := h.do(http.MethodGet, "/api/v1/config", nil, nil)
			if config.str("payment_mode") != mode {
				t.Fatalf("payment_mode = %q", config.str("payment_mode"))
			}
			h.openStore("standard")
			if _, purchasable, enabled := h.listedCodes(); purchasable["standard"] || enabled {
				t.Fatalf("purchasable = %v, enabled = %v", purchasable, enabled)
			}
			mustStatus(t, h.checkout(s, "standard"), http.StatusForbidden, "PURCHASES_DISABLED")
		})
	}
}
