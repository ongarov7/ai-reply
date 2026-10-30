package config

import (
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/payments/bcc"
)

func checkoutEnvFixture(t *testing.T) {
	t.Helper()
	setValidEnv(t)
	for key, value := range map[string]string{
		"PUBLIC_BASE_URL": "https://payment.example.test", "BCC_CHECKOUT_ENABLED": "false",
		"BCC_CHECKOUT_ENVIRONMENT": "sandbox", "BCC_CHECKOUT_PRODUCTION_APPROVED": "false",
		"BCC_SANDBOX_MERCHANT_ID": "00000001", "BCC_SANDBOX_MERCHANT_NAME": "TEST MERCHANT",
		"BCC_SANDBOX_TERMINAL_ID": "88888881", "BCC_SANDBOX_MAC_KEY": "11223344556677889900AABBCCDDEEFF",
		"BCC_CHECKOUT_NOTIFY_USERNAME": "sandbox-bank", "BCC_CHECKOUT_NOTIFY_PASSWORD": strings.Repeat("s", 40), "BCC_CHECKOUT_STATE_SECRET": strings.Repeat("t", 40),
		"BCC_PRODUCTION_MERCHANT_ID": "12345678", "BCC_PRODUCTION_MERCHANT_NAME": "LIVE MERCHANT",
		"BCC_PRODUCTION_TERMINAL_ID": "12345678", "BCC_PRODUCTION_MAC_KEY": "00112233445566778899AABBCCDDEEFF",
		"BCC_PRODUCTION_NOTIFY_USERNAME": "live-bank", "BCC_PRODUCTION_NOTIFY_PASSWORD": strings.Repeat("p", 40), "BCC_PRODUCTION_STATE_SECRET": strings.Repeat("q", 40),
		"BILLING_SANDBOX_ENABLED": "false", "BILLING_SANDBOX_BCC_ENABLED": "false",
	} {
		t.Setenv(key, value)
	}
}

func productionCheckoutEnvFixture(t *testing.T) {
	t.Helper()
	checkoutEnvFixture(t)
	for key, value := range map[string]string{
		"APP_ENV": "production", "AUTH_DEMO_MODE": "false", "PAYMENT_MODE": "off",
		"BCC_CHECKOUT_ENABLED": "true", "BCC_CHECKOUT_ENVIRONMENT": "production", "BCC_CHECKOUT_PRODUCTION_APPROVED": "true",
		"RESEND_API_KEY": "re_dummy_fixture", "RESEND_FROM_EMAIL": "no-reply@example.test",
		"APPLE_IAP_ENABLED": "false", "APPLE_IAP_ACCEPT_SANDBOX": "false",
	} {
		t.Setenv(key, value)
	}
}

func TestBCCCheckoutStaysDisabledAndUsesSeparateEnvironmentCredentials(t *testing.T) {
	checkoutEnvFixture(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BCCCheckout.Enabled || cfg.BCCCheckout.ProductionApproved || cfg.BCCCheckout.Environment != bcc.Sandbox || cfg.BCCSandboxCheckout.Enabled {
		t.Fatal("bank checkout enabled implicitly")
	}
	productionCheckoutEnvFixture(t)
	cfg, err = Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BCCCheckout.Environment != bcc.Production || cfg.BCCCheckout.Terminal != "12345678" || cfg.BCCCheckout.NotifyUsername != "live-bank" {
		t.Fatal("production selected sandbox credentials")
	}
	if cfg.BCCSandboxCheckout.Environment != bcc.Sandbox || cfg.BCCSandboxCheckout.Terminal != "88888881" || cfg.BCCSandboxCheckout.NotifyUsername != "sandbox-bank" || cfg.BCCSandboxCheckout.Enabled {
		t.Fatal("child sandbox inherited production credentials or enable flag")
	}
}

func TestBCCProductionRejectsMissingApprovalAndReusedSandboxCredentials(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"no approval", "BCC_CHECKOUT_PRODUCTION_APPROVED", "false"},
		{"missing live MAC", "BCC_PRODUCTION_MAC_KEY", ""},
		{"reused terminal", "BCC_PRODUCTION_TERMINAL_ID", "88888881"},
		{"reused MAC", "BCC_PRODUCTION_MAC_KEY", "11223344556677889900aabbccddeeff"},
		{"reused notify secret", "BCC_PRODUCTION_NOTIFY_PASSWORD", strings.Repeat("s", 40)},
		{"reused state secret", "BCC_PRODUCTION_STATE_SECRET", strings.Repeat("t", 40)},
		{"mixed state secret", "BCC_PRODUCTION_STATE_SECRET", strings.Repeat("s", 40)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			productionCheckoutEnvFixture(t)
			t.Setenv(tc.key, tc.value)
			if _, err := Load(""); err == nil {
				t.Fatal("unsafe production configuration accepted")
			}
		})
	}
}
