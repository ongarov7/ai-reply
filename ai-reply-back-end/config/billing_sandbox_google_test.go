package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBillingSandboxGoogleRequiresExplicitIndependentCredentials(t *testing.T) {
	setValidEnv(t)
	t.Setenv("GOOGLE_PLAY_CLIENT_EMAIL", "primary@example.invalid")
	t.Setenv("GOOGLE_PLAY_PRIVATE_KEY", "primary-fixture-key")
	t.Setenv("GOOGLE_PLAY_TOKEN_SECRET", strings.Repeat("p", 40))
	t.Setenv("BILLING_SANDBOX_ENABLED", "true")
	t.Setenv("BILLING_SANDBOX_GOOGLE_ENABLED", "false")
	t.Setenv("BILLING_SANDBOX_GOOGLE_SERVICE_ACCOUNT_FILE", "/does-not-exist/disabled.json")
	child := billingSandboxGoogleFromEnv()
	if child.Google() || child.GooglePrivateKey != "" || child.GoogleServiceAccountFile != "" {
		t.Fatal("disabled sandbox read or inherited credentials")
	}
	t.Setenv("BILLING_SANDBOX_GOOGLE_ENABLED", "true")
	t.Setenv("BILLING_SANDBOX_GOOGLE_SERVICE_ACCOUNT_FILE", "")
	t.Setenv("BILLING_SANDBOX_GOOGLE_TOKEN_SECRET", strings.Repeat("s", 40))
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "BILLING_SANDBOX_GOOGLE_SERVICE_ACCOUNT_FILE") {
		t.Fatal("sandbox fell back to primary credentials")
	}
	file := filepath.Join(t.TempDir(), "sandbox-play.json")
	if err := os.WriteFile(file, []byte(`{"type":"service_account","client_email":"sandbox@example.invalid","private_key":"sandbox-fixture-key"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BILLING_SANDBOX_GOOGLE_SERVICE_ACCOUNT_FILE", file)
	t.Setenv("BILLING_SANDBOX_GOOGLE_VOIDED_POLL", "15m")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.BillingSandboxGoogleEnabled || !cfg.BillingSandboxGoogle.Google() || !cfg.BillingSandboxGoogle.GoogleTestOnly || !cfg.BillingSandboxGoogle.GoogleAcceptTest ||
		cfg.BillingSandboxGoogle.GoogleClientEmail != "sandbox@example.invalid" || cfg.BillingSandboxGoogle.GoogleTokenSecret != strings.Repeat("s", 40) ||
		cfg.BillingSandboxGoogle.GoogleVoidedPoll != 15*time.Minute || cfg.IAP.GoogleClientEmail != "primary@example.invalid" || cfg.IAP.GoogleTestOnly {
		t.Fatal("sandbox and primary Google configuration crossed")
	}
	for name, value := range map[string]string{"BILLING_SANDBOX_ENABLED": "false", "BILLING_SANDBOX_GOOGLE_TOKEN_SECRET": "short", "BILLING_SANDBOX_GOOGLE_VOIDED_POLL": "off", "BILLING_SANDBOX_GOOGLE_PACKAGE_NAME": "bad..package"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, value)
			if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "BILLING_SANDBOX") || strings.Contains(err.Error(), "sandbox-fixture-key") {
				t.Fatalf("invalid sandbox configuration accepted or exposed credentials: %v", err)
			}
		})
	}
	t.Setenv("BILLING_SANDBOX_GOOGLE_TOKEN_SECRET", cfg.Auth.AccessSecret)
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "must be separate") {
		t.Fatal("sandbox accepted a primary sealing secret")
	}
}
