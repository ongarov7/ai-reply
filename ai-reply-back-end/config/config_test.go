package config

import (
	"os"
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

// OPENAI_TEMPERATURE: орнатылмаса не бос болса 0.7 (бұрынғыдай), "none" — өріс жіберілмейді.
func TestOpenAITemperatureIsOptional(t *testing.T) {
	setValidEnv(t)
	t.Setenv("OPENAI_TEMPERATURE", "") // restores the original value afterwards
	if err := os.Unsetenv("OPENAI_TEMPERATURE"); err != nil {
		t.Fatal(err)
	}
	if cfg, err := Load(""); err != nil || cfg.OpenAI.Temperature == nil || *cfg.OpenAI.Temperature != 0.7 {
		t.Fatalf("unset: %v %v", cfg.OpenAI.Temperature, err)
	}
	for raw, want := range map[string]float64{"0.3": 0.3, " 1 ": 1, "warm": 0.7, "": 0.7, "  ": 0.7} {
		t.Setenv("OPENAI_TEMPERATURE", raw)
		if cfg, err := Load(""); err != nil || cfg.OpenAI.Temperature == nil || *cfg.OpenAI.Temperature != want {
			t.Fatalf("%q: %v %v", raw, cfg.OpenAI.Temperature, err)
		}
	}
	for _, raw := range []string{"none", "None", " NONE "} {
		t.Setenv("OPENAI_TEMPERATURE", raw)
		if cfg, err := Load(""); err != nil || cfg.OpenAI.Temperature != nil {
			t.Fatalf("%q must omit the temperature: %v %v", raw, cfg.OpenAI.Temperature, err)
		}
	}
}

func TestAIQualityDefaults(t *testing.T) {
	setValidEnv(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AI.RepairEnabled || !cfg.AI.PolishEnabled || cfg.Limits.PolishPerMinute != 20 {
		t.Fatalf("defaults = %+v, polish %d/min", cfg.AI, cfg.Limits.PolishPerMinute)
	}
	t.Setenv("AI_REPAIR_ENABLED", "false")
	t.Setenv("AI_POLISH_ENABLED", "0")
	t.Setenv("RATE_POLISH_PER_MINUTE", "5")
	if cfg, _ := Load(""); cfg.AI.RepairEnabled || cfg.AI.PolishEnabled || cfg.Limits.PolishPerMinute != 5 {
		t.Fatalf("overrides = %+v, polish %d/min", cfg.AI, cfg.Limits.PolishPerMinute)
	}
}

func TestProductEventDefaults(t *testing.T) {
	setValidEnv(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Analytics.ProductEventsEnabled || cfg.Limits.EventsPerMinute != 30 {
		t.Fatalf("defaults = %+v, events %d/min", cfg.Analytics, cfg.Limits.EventsPerMinute)
	}
	t.Setenv("PRODUCT_EVENTS_ENABLED", "false")
	t.Setenv("RATE_EVENTS_PER_MINUTE", "10")
	if cfg, _ := Load(""); cfg.Analytics.ProductEventsEnabled || cfg.Limits.EventsPerMinute != 10 {
		t.Fatalf("overrides = %+v, events %d/min", cfg.Analytics, cfg.Limits.EventsPerMinute)
	}
}

// Push провайдерсіз сервер жұмыс істейді: әдепкі баптау — өшірулі.
func TestPushIsOffByDefaultAndNeedsNoCredentials(t *testing.T) {
	setValidEnv(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p := cfg.Push
	if p.Enabled || p.FCM.Configured() || !p.WorkerEnabled || !p.EmailsEnabled {
		t.Fatalf("push must be off without configuration: %+v", p)
	}
	if p.MaxAttempts != 5 || p.BatchSize != 50 || p.Concurrency != 8 || p.CampaignsPerHour != 10 ||
		p.QuotaLowPercent != 10 || p.RetentionDays != 180 {
		t.Fatalf("defaults: %+v", p)
	}
	if len(p.LinkHosts) != 1 || p.LinkHosts[0] != "ai-reply.kz" {
		t.Fatalf("link hosts default to the public host: %v", p.LinkHosts)
	}

	t.Setenv("PUSH_QUOTA_LOW_PERCENT", "25")
	t.Setenv("RETENTION_NOTIFICATIONS_DAYS", "0")
	t.Setenv("NOTIFICATION_EMAILS_ENABLED", "false")
	t.Setenv("PUSH_LINK_HOSTS", "ai-reply.kz, help.ai-reply.kz")
	cfg, err = Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Push.QuotaLowPercent != 25 || cfg.Push.RetentionDays != 0 || cfg.Push.EmailsEnabled ||
		len(cfg.Push.LinkHosts) != 2 {
		t.Fatalf("overrides: %+v", cfg.Push)
	}
}

// Жартылай толтырылған провайдер не мағынасыз шек — баптау қатесі, сервер іске қосылмайды.
func TestPushRejectsHalfConfiguredFirebaseAndBadLimits(t *testing.T) {
	setValidEnv(t)
	t.Setenv("FIREBASE_PROJECT_ID", "ai-reply")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "FIREBASE_CLIENT_EMAIL") {
		t.Fatalf("half FCM config must fail, err = %v", err)
	}
	t.Setenv("FIREBASE_CLIENT_EMAIL", "push@ai-reply.iam.gserviceaccount.com")
	t.Setenv("FIREBASE_PRIVATE_KEY", `-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----`)
	cfg, err := Load("")
	if err != nil || !cfg.Push.FCM.Configured() || !strings.Contains(cfg.Push.FCM.PrivateKey, "\nAAAA\n") {
		t.Fatalf("complete FCM config: %v", err)
	}

	for key, value := range map[string]string{"PUSH_QUOTA_LOW_PERCENT": "101", "PUSH_MAX_ATTEMPTS": "11"} {
		setValidEnv(t)
		t.Setenv(key, value)
		if _, err := Load(""); err == nil || !strings.Contains(err.Error(), key) {
			t.Fatalf("%s=%s accepted (err %v)", key, value, err)
		}
	}
}

// Firebase қызметтік тіркелгісінің JSON файлы бір рет оқылады; қате мәтінінде кілт жоқ.
func TestFirebaseServiceAccountFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := dir + "/" + name
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	valid := write("sa.json", `{"type":"service_account","project_id":"ai-reply",
		"client_email":"push@ai-reply.iam.gserviceaccount.com",
		"private_key":"-----BEGIN PRIVATE KEY-----\nc2VjcmV0LWtleQ==\n-----END PRIVATE KEY-----\n"}`)

	setValidEnv(t)
	t.Setenv("FIREBASE_SERVICE_ACCOUNT_FILE", valid)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Push.FCM.Configured() || cfg.Push.FCM.ProjectID != "ai-reply" || cfg.Push.FCM.ServiceAccountFile != valid ||
		!strings.HasPrefix(cfg.Push.FCM.PrivateKey, "-----BEGIN PRIVATE KEY-----\n") {
		t.Fatalf("fcm = %+v", cfg.Push.FCM.ProjectID)
	}

	for name, path := range map[string]string{
		"missing":    dir + "/nope.json",
		"not json":   write("broken.json", `c2VjcmV0LWtleQ== not json`),
		"no key":     write("nokey.json", `{"type":"service_account","project_id":"ai-reply","client_email":"a@b"}`),
		"wrong kind": write("user.json", `{"type":"authorized_user","project_id":"p","client_email":"a@b","private_key":"c2VjcmV0LWtleQ=="}`),
	} {
		setValidEnv(t)
		t.Setenv("FIREBASE_SERVICE_ACCOUNT_FILE", path)
		_, err := Load("")
		if err == nil || !strings.Contains(err.Error(), "FIREBASE_SERVICE_ACCOUNT_FILE") || strings.Contains(err.Error(), "c2VjcmV0") {
			t.Fatalf("%s: err = %v", name, err)
		}
	}

	setValidEnv(t)
	t.Setenv("FIREBASE_SERVICE_ACCOUNT_FILE", valid)
	t.Setenv("FIREBASE_PROJECT_ID", "other")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("file and variables together must fail, err = %v", err)
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
