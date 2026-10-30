package config

import "github.com/aireply/ai-reply-back-end/internal/payments/bcc"

type BCCCheckout = bcc.CheckoutConfig

func bccCheckoutFromEnv(appEnv, publicBaseURL string) BCCCheckout {
	return BCCCheckout{
		Enabled:        boolean("BCC_CHECKOUT_ENABLED", false),
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
