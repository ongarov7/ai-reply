package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func validBCCNotify(t *testing.T) {
	t.Helper()
	setValidEnv(t)
	t.Setenv("PUBLIC_BASE_URL", "https://ai-reply.kz")
	t.Setenv("BCC_SANDBOX_NOTIFY_ENABLED", "true")
	t.Setenv("BCC_TEST_NOTIFY_URL", "https://ai-reply.kz:443/api/v1/payments/bcc/sandbox/notify")
	t.Setenv("BCC_SANDBOX_NOTIFY_MANAGEMENT_TOKEN", base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("m", 32))))
}

func TestBCCSandboxNotifyDefaultOff(t *testing.T) {
	setValidEnv(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BCCSandboxNotify.Enabled {
		t.Fatal("sandbox notification receiver is on by default")
	}
}

func TestBCCSandboxNotifyLoadAndValidate(t *testing.T) {
	validBCCNotify(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.BCCSandboxNotify.Enabled || cfg.BCCSandboxNotify.PublicURL != "https://ai-reply.kz:443/api/v1/payments/bcc/sandbox/notify" || cfg.BCCSandboxNotify.ManagementToken == "" {
		t.Fatalf("notification settings not loaded: %v", cfg.BCCSandboxNotify)
	}
	for name, value := range map[string]string{
		"BCC_TEST_NOTIFY_URL":                 "",
		"BCC_SANDBOX_NOTIFY_MANAGEMENT_TOKEN": "",
		"BCC_SANDBOX_NOTIFY_BASIC_USERNAME":   "configured-without-password",
	} {
		t.Run(name, func(t *testing.T) {
			validBCCNotify(t)
			t.Setenv(name, value)
			if _, err := Load(""); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("partial notification configuration accepted: %v", err)
			}
		})
	}
}

func TestBCCSandboxNotifyURLMustUseExistingHTTPSHostAndStablePath(t *testing.T) {
	for _, value := range []string{
		"https://ai-reply.kz/api/v1/payments/bcc/sandbox/notify",
		"http://ai-reply.kz:443/api/v1/payments/bcc/sandbox/notify",
		"https://another.example:443/api/v1/payments/bcc/sandbox/notify",
		"https://ai-reply.kz:443/api/v1/payments/bcc/sandbox/notify/random-path",
		"https://ai-reply.kz:443/api/v1/payments/bcc/sandbox/notify?token=secret",
		"https://user:secret@ai-reply.kz:443/api/v1/payments/bcc/sandbox/notify",
	} {
		t.Run(value, func(t *testing.T) {
			validBCCNotify(t)
			t.Setenv("BCC_TEST_NOTIFY_URL", value)
			if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "BCC_TEST_NOTIFY_URL") {
				t.Fatal("invalid or different callback URL accepted")
			}
		})
	}
}

func TestBCCSandboxNotifyCredentialSeparationAndFormatting(t *testing.T) {
	validBCCNotify(t)
	token := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("m", 32)))
	t.Setenv("JWT_ACCESS_SECRET", token)
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "must be separate") {
		t.Fatal("application credential reused for management")
	}
	validBCCNotify(t)
	t.Setenv("BCC_SANDBOX_NOTIFY_BASIC_USERNAME", "bank-notify")
	t.Setenv("BCC_SANDBOX_NOTIFY_BASIC_PASSWORD", strings.Repeat("bank-password", 4))
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(cfg.BCCSandboxNotify)
	if err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{fmt.Sprint(cfg.BCCSandboxNotify), fmt.Sprintf("%#v", cfg.BCCSandboxNotify), string(encoded), fmt.Sprintf("%+v", cfg)} {
		if strings.Contains(rendered, token) || strings.Contains(rendered, cfg.BCCSandboxNotify.BasicPassword) {
			t.Fatal("sandbox credentials leaked through config formatting")
		}
	}
}
