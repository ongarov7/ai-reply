package config

import "github.com/aireply/ai-reply-back-end/internal/payments/bcc"

type BCCCheckout = bcc.CheckoutConfig

func bccCheckoutFromEnv(appEnv, publicBaseURL string) BCCCheckout {
	cfg := bccSandboxCheckoutFromEnv(appEnv, publicBaseURL)
	cfg.Enabled = boolean("BCC_CHECKOUT_ENABLED", false)
	cfg.Environment = bcc.Environment(str("BCC_CHECKOUT_ENVIRONMENT", "sandbox"))
	if cfg.Environment == bcc.Production {
		cfg.ProductionApproved = boolean("BCC_CHECKOUT_PRODUCTION_APPROVED", false)
		cfg.Merchant = str("BCC_PRODUCTION_MERCHANT_ID", "")
		cfg.MerchantName = str("BCC_PRODUCTION_MERCHANT_NAME", "")
		cfg.Terminal = str("BCC_PRODUCTION_TERMINAL_ID", "")
		cfg.MACKeyHex = str("BCC_PRODUCTION_MAC_KEY", "")
		cfg.NotifyUsername = str("BCC_PRODUCTION_NOTIFY_USERNAME", "")
		cfg.NotifyPassword = str("BCC_PRODUCTION_NOTIFY_PASSWORD", "")
		cfg.StateSecret = str("BCC_PRODUCTION_STATE_SECRET", "")
	}
	return cfg
}

// Keep the child configuration independent of the primary checkout selection.
// Only billingSandboxConfig enables it and fixes its URL and app environment.
func bccSandboxCheckoutFromEnv(appEnv, publicBaseURL string) BCCCheckout {
	return BCCCheckout{
		Environment:    bcc.Sandbox,
		AppEnv:         appEnv,
		PublicBaseURL:  publicBaseURL,
		Merchant:       str("BCC_SANDBOX_MERCHANT_ID", ""),
		MerchantName:   str("BCC_SANDBOX_MERCHANT_NAME", ""),
		Terminal:       str("BCC_SANDBOX_TERMINAL_ID", ""),
		MACKeyHex:      str("BCC_SANDBOX_MAC_KEY", ""),
		NotifyUsername: str("BCC_CHECKOUT_NOTIFY_USERNAME", ""),
		NotifyPassword: str("BCC_CHECKOUT_NOTIFY_PASSWORD", ""),
		StateSecret:    str("BCC_CHECKOUT_STATE_SECRET", ""),
	}
}
