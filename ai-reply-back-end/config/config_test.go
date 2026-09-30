package config

import (
	"strings"
	"testing"
)

// setValidEnv — Validate тексеруінен өтетін ең аз баптау.
func setValidEnv(t *testing.T) {
	t.Helper()
	env := map[string]string{
		"APP_ENV": "development", "APP_HOST": "0.0.0.0", "APP_PORT": "8080",
		"DEFAULT_TIMEZONE": "Asia/Almaty", "PAYMENT_MODE": "demo",
		"OPENAI_API_KEY":      "sk-test-key",
		"JWT_ACCESS_SECRET":   "test-access-secret-that-is-long-enough-000",
		"JWT_REFRESH_SECRET":  "test-refresh-secret-that-is-long-enough-0",
		"AUTH_SIGNING_SECRET": "test-legacy-secret-that-is-long-enough-00",
		"AUTH_DEMO_MODE":      "true", "AUTH_DEMO_OTP": "1111", "ADMIN_EMAIL": "",
	}
	for key, value := range env {
		t.Setenv(key, value)
	}
}

func TestLoadUsesAppPort(t *testing.T) {
	setValidEnv(t)
	t.Setenv("APP_PORT", "8084")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.App.Host != "0.0.0.0" || cfg.App.Port != 8084 {
		t.Fatalf("addr = %s:%d, want 0.0.0.0:8084", cfg.App.Host, cfg.App.Port)
	}
}

// Порт жоқ не қате болса, сервер әдепкі портқа үнсіз ауыспай, іске қосылмайды.
func TestLoadRejectsInvalidAppPort(t *testing.T) {
	for _, raw := range []string{"", "  ", "abc", "80a", "8.5", "0", "-1", "65536"} {
		setValidEnv(t)
		t.Setenv("APP_PORT", raw)

		if cfg, err := Load(""); err == nil || !strings.Contains(err.Error(), "APP_PORT") {
			t.Fatalf("APP_PORT=%q: Load() = port %d, err %v; want APP_PORT error", raw, cfg.App.Port, err)
		}
	}
}

func TestProductionRequiresEmailDelivery(t *testing.T) {
	setValidEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("AUTH_DEMO_MODE", "false")
	t.Setenv("PAYMENT_MODE", "live")
	t.Setenv("PUBLIC_BASE_URL", "https://ai-reply.kz")
	t.Setenv("RESEND_API_KEY", "")
	t.Setenv("RESEND_FROM_EMAIL", "")

	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "RESEND_API_KEY") {
		t.Fatalf("production without Resend must not start, err = %v", err)
	}

	t.Setenv("RESEND_API_KEY", "re_test_key")
	t.Setenv("RESEND_FROM_EMAIL", "NoReply@AI-Reply.kz")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Email.Enabled() || cfg.Email.FromEmail != "noreply@ai-reply.kz" || cfg.Email.FromName != "AI Reply" {
		t.Fatalf("email = %+v", cfg.Email)
	}
}

func TestResendSenderMustBeAPlainAddress(t *testing.T) {
	for _, from := range []string{"AI Reply <noreply@ai-reply.kz>", "noreply", ""} {
		setValidEnv(t)
		t.Setenv("RESEND_API_KEY", "re_test_key")
		t.Setenv("RESEND_FROM_EMAIL", from)
		if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "RESEND_FROM_EMAIL") {
			t.Fatalf("RESEND_FROM_EMAIL=%q accepted (err %v)", from, err)
		}
	}
	// Outside production a sender without a key just leaves delivery off, so
	// `cp .env.example .env` still starts a local server.
	setValidEnv(t)
	t.Setenv("RESEND_API_KEY", "")
	t.Setenv("RESEND_FROM_EMAIL", "noreply@ai-reply.kz")
	cfg, err := Load("")
	if err != nil || cfg.Email.Enabled() {
		t.Fatalf("Load = %+v, %v; want delivery off without a key", cfg.Email, err)
	}
}

func TestSignInSettingsFromEnvironment(t *testing.T) {
	setValidEnv(t)
	t.Setenv("GOOGLE_CLIENT_ID_IOS", "123-ios.apps.googleusercontent.com")
	t.Setenv("GOOGLE_CLIENT_ID_WEB", "123-web.apps.googleusercontent.com")
	t.Setenv("APPLE_CLIENT_ID", "kz.yerek.replykeyboard")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.OAuth.GoogleClientIDs) != 2 || cfg.OAuth.GoogleClientIDs[0] != "123-ios.apps.googleusercontent.com" ||
		cfg.OAuth.GoogleClientIDs[1] != "123-web.apps.googleusercontent.com" {
		t.Fatalf("google = %v", cfg.OAuth.GoogleClientIDs)
	}
	if len(cfg.OAuth.AppleClientIDs) != 1 || cfg.OAuth.AppleClientIDs[0] != "kz.yerek.replykeyboard" {
		t.Fatalf("apple = %v", cfg.OAuth.AppleClientIDs)
	}
	if cfg.Auth.OTPResendCooldown.Seconds() != 32 || cfg.Auth.OTPRequestsPerDay != 10 || cfg.Auth.OTPTTL.Minutes() != 5 {
		t.Fatalf("otp defaults = %v / %d / %v", cfg.Auth.OTPResendCooldown, cfg.Auth.OTPRequestsPerDay, cfg.Auth.OTPTTL)
	}
}

func TestDemoOTPMustBeFourDigits(t *testing.T) {
	for _, code := range []string{"111", "11111", "12a4"} {
		setValidEnv(t)
		t.Setenv("AUTH_DEMO_OTP", code)
		if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "AUTH_DEMO_OTP") {
			t.Fatalf("AUTH_DEMO_OTP=%q accepted", code)
		}
	}
}

// Push provider-сіз сервер жұмыс істейді: әдепкі баптау — өшірулі.
func TestPushIsOffByDefaultAndNeedsNoCredentials(t *testing.T) {
	setValidEnv(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Push.Enabled || cfg.Push.FCM.Configured() || cfg.Push.APNs.Configured() {
		t.Fatalf("push must be off without configuration: %+v", cfg.Push)
	}
	if cfg.Push.APNs.Environment != "production" || cfg.Push.MaxAttempts != 5 {
		t.Fatalf("defaults: %+v", cfg.Push)
	}
	if len(cfg.Push.LinkHosts) != 1 || cfg.Push.LinkHosts[0] != "ai-reply.kz" {
		t.Fatalf("link hosts default to the public host: %v", cfg.Push.LinkHosts)
	}
	if !cfg.Telemetry.Enabled || cfg.Telemetry.RetentionAuditLogDays != 0 || cfg.Telemetry.RetentionAppEventsDays != 90 {
		t.Fatalf("telemetry defaults: %+v", cfg.Telemetry)
	}
}

// Жартылай толтырылған провайдер — баптау қатесі, сервер іске қосылмайды.
func TestPushRejectsHalfConfiguredProviders(t *testing.T) {
	setValidEnv(t)
	t.Setenv("FIREBASE_PROJECT_ID", "ai-reply")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "FIREBASE_CLIENT_EMAIL") {
		t.Fatalf("half FCM config must fail, err = %v", err)
	}

	setValidEnv(t)
	t.Setenv("FIREBASE_PROJECT_ID", "")
	t.Setenv("APNS_KEY_ID", "ABC123DEFG")
	t.Setenv("APNS_TEAM_ID", "JXM8N66QWU")
	t.Setenv("APNS_PRIVATE_KEY", "-----BEGIN PRIVATE KEY-----\\nAAA\\n-----END PRIVATE KEY-----")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "APNS_BUNDLE_ID") {
		t.Fatalf("APNs without bundle id must fail, err = %v", err)
	}

	t.Setenv("APNS_BUNDLE_ID", "kz.yerek.replykeyboard")
	t.Setenv("APNS_ENVIRONMENT", "staging")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "APNS_ENVIRONMENT") {
		t.Fatalf("unknown APNs environment must fail, err = %v", err)
	}
	t.Setenv("APNS_ENVIRONMENT", "development")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("complete APNs config: %v", err)
	}
	if !cfg.Push.APNs.Configured() || cfg.Push.APNs.Environment != "sandbox" {
		t.Fatalf("apns = %+v", cfg.Push.APNs)
	}
}

// .env-те бір жолмен жазылған кілт қалыпты көп жолды PEM-ге айналады.
func TestPEMNormalisesEscapedAndBase64Keys(t *testing.T) {
	want := "-----BEGIN PRIVATE KEY-----\nMIIB\nAAAA\n-----END PRIVATE KEY-----\n"
	for name, raw := range map[string]string{
		"escaped":   `-----BEGIN PRIVATE KEY-----\nMIIB\nAAAA\n-----END PRIVATE KEY-----\n`,
		"crlf":      "-----BEGIN PRIVATE KEY-----\r\nMIIB\r\nAAAA\r\n-----END PRIVATE KEY-----",
		"multiline": "-----BEGIN PRIVATE KEY-----\nMIIB\nAAAA\n-----END PRIVATE KEY-----",
		"base64":    "LS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0tCk1JSUIKQUFBQQotLS0tLUVORCBQUklWQVRFIEtFWS0tLS0tCg==",
	} {
		if got := PEM(raw); got != want {
			t.Errorf("%s: got %q", name, got)
		}
	}
	if PEM("  ") != "" {
		t.Error("empty stays empty")
	}
}
