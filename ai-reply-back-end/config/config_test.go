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
		"CONTACT_EMAIL": "support@ai-reply.kz",
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
	// Outside production a sender without a key just leaves delivery off, so a
	// local server needs no Resend account.
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
	t.Setenv("APPLE_CLIENT_ID", "kz.ai-reply.reply.keyboard.keyboard")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.OAuth.GoogleClientIDs) != 2 || cfg.OAuth.GoogleClientIDs[0] != "123-ios.apps.googleusercontent.com" ||
		cfg.OAuth.GoogleClientIDs[1] != "123-web.apps.googleusercontent.com" {
		t.Fatalf("google = %v", cfg.OAuth.GoogleClientIDs)
	}
	if len(cfg.OAuth.AppleClientIDs) != 1 || cfg.OAuth.AppleClientIDs[0] != "kz.ai-reply.reply.keyboard.keyboard" {
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

// unset — айнымалыны тест біткенше алып тастайды (t.Setenv кейін қалпына келтіреді).
func unset(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

// Шығарылымға қауіпсіз әдепкілер: демо кіру, ескі API және төлем өшірулі.
func TestReleaseDefaults(t *testing.T) {
	setValidEnv(t)
	unset(t, "AUTH_DEMO_MODE", "LEGACY_API_ENABLED", "PAYMENT_MODE", "PAYMENT_DEMO_CHECKOUT", "CONTACT_EMAIL",
		"RATE_AI_REPORTS_PER_HOUR", "LEGAL_OPERATOR_NAME", "LEGAL_OPERATOR_DETAILS")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.DemoMode || cfg.Auth.LegacyEnabled || cfg.Payments.Mode != "off" || cfg.Payments.DemoCheckout {
		t.Fatalf("defaults: demo %v legacy %v payments %q/%v", cfg.Auth.DemoMode, cfg.Auth.LegacyEnabled,
			cfg.Payments.Mode, cfg.Payments.DemoCheckout)
	}
	if cfg.App.ContactEmail() != "" || cfg.Legal.OperatorName != "" || cfg.Limits.ReportsPerHour != 20 {
		t.Fatalf("contact %q, operator %q, reports %d/h", cfg.App.ContactEmail(), cfg.Legal.OperatorName, cfg.Limits.ReportsPerHour)
	}
	if r := cfg.Retention; r.OTPDays != 30 || r.RefreshTokenDays != 30 || r.AnonInstallationsDays != 180 ||
		r.ProductEventsDays != 400 || r.AIUsageEventsDays != 400 {
		t.Fatalf("retention defaults = %+v", r)
	}
}

// Демо кіру (тұрақты код) тек development пен test ортасында.
func TestDemoModeOnlyForDevelopmentAndTest(t *testing.T) {
	for env, ok := range map[string]bool{"development": true, "test": true, "staging": false, "production": false} {
		setValidEnv(t)
		t.Setenv("APP_ENV", env)
		t.Setenv("PAYMENT_MODE", "off")
		t.Setenv("RESEND_API_KEY", "re_test_key")
		t.Setenv("RESEND_FROM_EMAIL", "noreply@ai-reply.kz")
		_, err := Load("")
		if ok && err != nil {
			t.Errorf("%s: %v", env, err)
		}
		if !ok && (err == nil || !strings.Contains(err.Error(), "AUTH_DEMO_MODE=true is allowed only")) {
			t.Errorf("%s accepted demo mode (err %v)", env, err)
		}
	}
}

func TestPaymentModes(t *testing.T) {
	for mode, ok := range map[string]bool{"off": true, "OFF": true, "demo": true, "live": true, "free": false} {
		setValidEnv(t)
		t.Setenv("PAYMENT_MODE", mode)
		if _, err := Load(""); (err == nil) != ok {
			t.Errorf("PAYMENT_MODE=%s: err %v", mode, err)
		}
	}
	// Demo checkout only with the demo provider.
	for mode, ok := range map[string]bool{"demo": true, "off": false, "live": false} {
		setValidEnv(t)
		t.Setenv("PAYMENT_MODE", mode)
		t.Setenv("PAYMENT_DEMO_CHECKOUT", "true")
		_, err := Load("")
		if ok != (err == nil) || (!ok && !strings.Contains(err.Error(), "PAYMENT_DEMO_CHECKOUT=true needs PAYMENT_MODE=demo")) {
			t.Errorf("checkout with %s: err %v", mode, err)
		}
	}
	// Production allows off and live only.
	for mode, ok := range map[string]bool{"off": true, "live": true, "demo": false} {
		setValidEnv(t)
		t.Setenv("APP_ENV", "production")
		t.Setenv("AUTH_DEMO_MODE", "false")
		t.Setenv("PAYMENT_DEMO_CHECKOUT", "false")
		t.Setenv("RESEND_API_KEY", "re_test_key")
		t.Setenv("RESEND_FROM_EMAIL", "noreply@ai-reply.kz")
		t.Setenv("PAYMENT_MODE", mode)
		if _, err := Load(""); (err == nil) != ok {
			t.Errorf("production PAYMENT_MODE=%s: err %v", mode, err)
		}
	}
}

// Apple токенін кері қайтару кілті: үш мән бірге және APPLE_CLIENT_ID-мен; кілт қате мәтінінде жоқ.
func TestAppleRevocationKeysGoTogether(t *testing.T) {
	setValidEnv(t)
	t.Setenv("APPLE_CLIENT_ID", "kz.ai-reply.app")
	t.Setenv("APPLE_TEAM_ID", "TEAM123")
	_, err := Load("")
	if err == nil || !strings.Contains(err.Error(), "APPLE_TEAM_ID, APPLE_KEY_ID and APPLE_PRIVATE_KEY must be set together") {
		t.Fatalf("half Apple key accepted: %v", err)
	}
	t.Setenv("APPLE_KEY_ID", "KEY123")
	t.Setenv("APPLE_PRIVATE_KEY", `-----BEGIN PRIVATE KEY-----\nc2VjcmV0\n-----END PRIVATE KEY-----`)
	cfg, err := Load("")
	if err != nil || !cfg.OAuth.AppleRevocation() || !strings.Contains(cfg.OAuth.ApplePrivateKey, "\nc2VjcmV0\n") {
		t.Fatalf("complete Apple key: %v", err)
	}
	unset(t, "APPLE_CLIENT_ID")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "APPLE_CLIENT_ID") || strings.Contains(err.Error(), "c2VjcmV0") {
		t.Fatalf("revocation key without a client id: %v", err)
	}
}

func TestContactEmailMustBeAPlainAddress(t *testing.T) {
	for raw, ok := range map[string]bool{"Support@AI-Reply.kz": true, "AI Reply <support@ai-reply.kz>": false, "support": false} {
		setValidEnv(t)
		t.Setenv("CONTACT_EMAIL", raw)
		cfg, err := Load("")
		if ok && (err != nil || cfg.App.ContactEmail() != "support@ai-reply.kz") {
			t.Errorf("%q: %q, %v", raw, cfg.App.ContactEmail(), err)
		}
		if !ok && (err == nil || !strings.Contains(err.Error(), "CONTACT_EMAIL")) {
			t.Errorf("%q accepted (err %v)", raw, err)
		}
	}
}

func TestLegalOperatorAndRetentionFromEnvironment(t *testing.T) {
	setValidEnv(t)
	t.Setenv("LEGAL_OPERATOR_NAME", "Operator Name")
	t.Setenv("LEGAL_OPERATOR_DETAILS", "Address, registration 123")
	t.Setenv("RETENTION_OTP_DAYS", "7")
	t.Setenv("RETENTION_REFRESH_TOKENS_DAYS", "0")
	t.Setenv("RETENTION_ANON_INSTALLATIONS_DAYS", "90")
	t.Setenv("RETENTION_PRODUCT_EVENTS_DAYS", "-5")
	t.Setenv("RETENTION_AI_USAGE_EVENTS_DAYS", "365")
	t.Setenv("RATE_AI_REPORTS_PER_HOUR", "5")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Legal.OperatorName != "Operator Name" || cfg.Legal.OperatorDetails != "Address, registration 123" {
		t.Fatalf("legal = %+v", cfg.Legal)
	}
	// 0 switches a window off; a negative value is not a window and keeps the default.
	if r := cfg.Retention; r.OTPDays != 7 || r.RefreshTokenDays != 0 || r.AnonInstallationsDays != 90 ||
		r.ProductEventsDays != 400 || r.AIUsageEventsDays != 365 || cfg.Limits.ReportsPerHour != 5 {
		t.Fatalf("retention = %+v, reports %d/h", r, cfg.Limits.ReportsPerHour)
	}
}

// Admin cookie-лері https сайтта әдепкіде Secure; айқын мән бәрібір басым. HSTS те https-пен бірге.
func TestSecureCookiesFollowTheHTTPSAddress(t *testing.T) {
	for _, tc := range []struct {
		base, explicit string
		secure, hsts   bool
	}{
		{"https://ai-reply.kz", "", true, true},
		{"http://localhost:8080", "", false, false},
		{"https://ai-reply.kz", "false", false, true},
		{"http://localhost:8080", "true", true, false},
	} {
		setValidEnv(t)
		t.Setenv("PUBLIC_BASE_URL", tc.base)
		if tc.explicit == "" {
			unset(t, "ADMIN_SECURE_COOKIES")
		} else {
			t.Setenv("ADMIN_SECURE_COOKIES", tc.explicit)
		}
		cfg, err := Load("")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Admin.SecureCookies != tc.secure || cfg.App.HSTS() != tc.hsts {
			t.Errorf("%s (ADMIN_SECURE_COOKIES=%q): secure %v, hsts %v", tc.base, tc.explicit, cfg.Admin.SecureCookies, cfg.App.HSTS())
		}
	}
}

// OTP: IP шектері кең (NAT), пошта шектері бөлек айнымалылармен және қатаң.
func TestOTPLimitDefaults(t *testing.T) {
	setValidEnv(t)
	unset(t, "RATE_OTP_REQUEST_PER_HOUR", "RATE_OTP_VERIFY_PER_HOUR", "RATE_OTP_REQUEST_PER_ADDRESS_PER_HOUR",
		"OTP_MAX_FAILED_PER_DAY", "LIMIT_POLISH_PER_DAY")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Limits.OTPRequestPerHour != 30 || cfg.Limits.OTPVerifyPerHour != 60 {
		t.Fatalf("per-IP defaults = %d / %d", cfg.Limits.OTPRequestPerHour, cfg.Limits.OTPVerifyPerHour)
	}
	if cfg.Auth.OTPRequestsPerHour != 5 || cfg.Auth.OTPRequestsPerDay != 10 || cfg.Auth.OTPMaxFailedPerDay != 10 {
		t.Fatalf("per-address defaults = %d / %d / %d", cfg.Auth.OTPRequestsPerHour, cfg.Auth.OTPRequestsPerDay,
			cfg.Auth.OTPMaxFailedPerDay)
	}
	if cfg.Limits.PolishPerDay != 100 {
		t.Fatalf("polish per day = %d", cfg.Limits.PolishPerDay)
	}
	// Raising the per-IP budget leaves the per-address cap alone.
	t.Setenv("RATE_OTP_REQUEST_PER_HOUR", "300")
	t.Setenv("OTP_MAX_FAILED_PER_DAY", "4")
	if cfg, err = Load(""); err != nil || cfg.Auth.OTPRequestsPerHour != 5 || cfg.Limits.OTPRequestPerHour != 300 ||
		cfg.Auth.OTPMaxFailedPerDay != 4 {
		t.Fatalf("overrides: %+v %v", cfg.Auth, err)
	}
}

// .env.example-дегі толтырғыштармен сервер іске қосылмайды; мән журналға шықпайды.
func TestPlaceholderSecretsAreRefused(t *testing.T) {
	for key, value := range map[string]string{
		"JWT_ACCESS_SECRET":   "REPLACE_WITH_32_RANDOM_BYTES_HEX_0000000",
		"JWT_REFRESH_SECRET":  "change_me-change_me-change_me-change_me",
		"OPENAI_API_KEY":      "sk-REPLACE_ME",
		"AUTH_SIGNING_SECRET": "Replace-With-A-Real-Legacy-Secret-000000",
		"ADMIN_PASSWORD":      "REPLACE_WITH_A_LONG_PASSPHRASE",
		"RESEND_API_KEY":      "re_CHANGE_ME",
	} {
		setValidEnv(t)
		t.Setenv("LEGACY_API_ENABLED", "true")
		t.Setenv("ADMIN_EMAIL", "admin@ai-reply.kz")
		t.Setenv("ADMIN_PASSWORD", "a-real-admin-passphrase")
		t.Setenv("RESEND_API_KEY", "re_test_key")
		t.Setenv("RESEND_FROM_EMAIL", "noreply@ai-reply.kz")
		t.Setenv(key, value)
		_, err := Load("")
		if err == nil || !strings.Contains(err.Error(), key+" still holds the placeholder") || strings.Contains(err.Error(), value) {
			t.Errorf("%s=%s: err = %v", key, value, err)
		}
	}
	// An unused legacy secret is not checked.
	setValidEnv(t)
	unset(t, "RESEND_API_KEY", "RESEND_FROM_EMAIL")
	t.Setenv("LEGACY_API_ENABLED", "false")
	t.Setenv("AUTH_SIGNING_SECRET", "REPLACE_WITH_32_RANDOM_BYTES_HEX")
	if _, err := Load(""); err != nil {
		t.Fatalf("unused legacy secret: %v", err)
	}
}

func TestLegacySecretMustDifferFromTheJWTSecrets(t *testing.T) {
	for _, other := range []string{"JWT_ACCESS_SECRET", "JWT_REFRESH_SECRET"} {
		setValidEnv(t)
		t.Setenv("LEGACY_API_ENABLED", "true")
		t.Setenv("AUTH_SIGNING_SECRET", os.Getenv(other))
		if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "AUTH_SIGNING_SECRET must differ") {
			t.Errorf("legacy secret equal to %s: err = %v", other, err)
		}
	}
}

// Production-да әкімші құпиясөзі кемінде 16 таңба.
func TestProductionAdminPasswordLength(t *testing.T) {
	production := func(password string) error {
		setValidEnv(t)
		t.Setenv("APP_ENV", "production")
		t.Setenv("AUTH_DEMO_MODE", "false")
		t.Setenv("PAYMENT_MODE", "off")
		t.Setenv("RESEND_API_KEY", "re_test_key")
		t.Setenv("RESEND_FROM_EMAIL", "noreply@ai-reply.kz")
		t.Setenv("PUBLIC_BASE_URL", "https://ai-reply.kz")
		t.Setenv("ADMIN_EMAIL", "admin@ai-reply.kz")
		t.Setenv("ADMIN_PASSWORD", password)
		_, err := Load("")
		return err
	}
	if err := production("fifteen-chars!!"); err == nil || !strings.Contains(err.Error(), "at least 16 characters when APP_ENV=production") {
		t.Fatalf("15 characters in production: %v", err)
	}
	if err := production("sixteen-chars!!!"); err != nil {
		t.Fatalf("16 characters in production: %v", err)
	}
	setValidEnv(t)
	t.Setenv("ADMIN_EMAIL", "admin@ai-reply.kz")
	t.Setenv("ADMIN_PASSWORD", "ten-chars!")
	if _, err := Load(""); err != nil {
		t.Fatalf("development keeps the 10-character minimum: %v", err)
	}
}

// Тексерушінің кіруі: екеуі бірге, бір пошта, 4 цифр; әдепкіде өшірулі.
func TestReviewLoginSettings(t *testing.T) {
	setValidEnv(t)
	unset(t, "REVIEW_LOGIN_EMAIL", "REVIEW_LOGIN_CODE")
	cfg, err := Load("")
	if err != nil || cfg.Auth.ReviewLogin() {
		t.Fatalf("review login must be off by default: %+v %v", cfg.Auth.ReviewEmail, err)
	}
	setValidEnv(t)
	t.Setenv("REVIEW_LOGIN_EMAIL", " Review@AI-Reply.kz ")
	t.Setenv("REVIEW_LOGIN_CODE", "4826")
	if cfg, err = Load(""); err != nil || !cfg.Auth.ReviewLogin() || cfg.Auth.ReviewEmail != "review@ai-reply.kz" {
		t.Fatalf("review login: %q %v", cfg.Auth.ReviewEmail, err)
	}
	for _, tc := range []struct{ email, code, want string }{
		{"review@ai-reply.kz", "", "must be set together"},
		{"", "4826", "must be set together"},
		{"review@ai-reply.kz, other@ai-reply.kz", "4826", "REVIEW_LOGIN_EMAIL must be exactly one plain address"},
		{"Review <review@ai-reply.kz>", "4826", "REVIEW_LOGIN_EMAIL must be exactly one plain address"},
		{"review@localhost", "4826", "REVIEW_LOGIN_EMAIL must be exactly one plain address"},
		{"review@ai-reply.kz", "48261", "REVIEW_LOGIN_CODE must be exactly 4 digits"},
		{"review@ai-reply.kz", "48a6", "REVIEW_LOGIN_CODE must be exactly 4 digits"},
	} {
		setValidEnv(t)
		t.Setenv("REVIEW_LOGIN_EMAIL", tc.email)
		t.Setenv("REVIEW_LOGIN_CODE", tc.code)
		if _, err := Load(""); err == nil || !strings.Contains(err.Error(), tc.want) || (tc.code != "" && strings.Contains(err.Error(), tc.code)) {
			t.Errorf("%q / %q: err = %v", tc.email, tc.code, err)
		}
	}
}

// Симулятор тек development пен test-те әдепкіде қосулы; айқын мән басым.
func TestSimulatorIsOffOutsideDevelopment(t *testing.T) {
	for env, want := range map[string]bool{"development": true, "test": true, "staging": false, "production": false} {
		setValidEnv(t)
		t.Setenv("APP_ENV", env)
		t.Setenv("AUTH_DEMO_MODE", "false")
		t.Setenv("PAYMENT_MODE", "off")
		t.Setenv("RESEND_API_KEY", "re_test_key")
		t.Setenv("RESEND_FROM_EMAIL", "noreply@ai-reply.kz")
		unset(t, "SIMULATOR_ENABLED")
		cfg, err := Load("")
		if err != nil || cfg.App.SimulatorEnabled != want {
			t.Errorf("%s: simulator %v, err %v", env, cfg.App.SimulatorEnabled, err)
		}
	}
	setValidEnv(t)
	t.Setenv("SIMULATOR_ENABLED", "false")
	if cfg, err := Load(""); err != nil || cfg.App.SimulatorEnabled {
		t.Fatalf("explicit false: %v %v", cfg.App.SimulatorEnabled, err)
	}
}

// Дүкен сілтемелері міндетті емес, бірақ берілсе — дүкеннің өз беті.
func TestStoreURLs(t *testing.T) {
	setValidEnv(t)
	t.Setenv("APP_STORE_URL", "https://apps.apple.com/kz/app/ai-reply/id0000000000")
	t.Setenv("PLAY_STORE_URL", "https://play.google.com/store/apps/details?id=kz.aireply")
	if cfg, err := Load(""); err != nil || cfg.App.AppStoreURL == "" || cfg.App.PlayStoreURL == "" {
		t.Fatalf("store urls: %v", err)
	}
	for key, value := range map[string]string{
		"APP_STORE_URL":  "http://apps.apple.com/app/id1",
		"PLAY_STORE_URL": "https://example.com/store?id=x",
	} {
		setValidEnv(t)
		t.Setenv(key, value)
		if _, err := Load(""); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%s=%s accepted (err %v)", key, value, err)
		}
	}
}

// setProductionEnv — production тексерулерінен өтетін ең аз баптау.
func setProductionEnv(t *testing.T) {
	t.Helper()
	setValidEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("AUTH_DEMO_MODE", "false")
	t.Setenv("PAYMENT_MODE", "off")
	t.Setenv("PUBLIC_BASE_URL", "https://ai-reply.kz")
	t.Setenv("RESEND_API_KEY", "re_test_key")
	t.Setenv("RESEND_FROM_EMAIL", "noreply@ai-reply.kz")
	unset(t, "LEGACY_API_ENABLED", "PAYMENT_DEMO_CHECKOUT", "LEGAL_OPERATOR_NAME", "APPLE_CLIENT_ID",
		"APPLE_TEAM_ID", "APPLE_KEY_ID", "APPLE_PRIVATE_KEY")
}

// Ескі /v1 API-де келісім жоқ: production оны қосып іске қосылмайды; басқа ортада болады.
func TestProductionRefusesLegacyAPI(t *testing.T) {
	setProductionEnv(t)
	if _, err := Load(""); err != nil {
		t.Fatalf("production baseline: %v", err)
	}
	t.Setenv("LEGACY_API_ENABLED", "true")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "LEGACY_API_ENABLED=true is not allowed when APP_ENV=production") {
		t.Fatalf("production with the legacy API: %v", err)
	}
	setValidEnv(t)
	t.Setenv("LEGACY_API_ENABLED", "true")
	if cfg, err := Load(""); err != nil || !cfg.Auth.LegacyEnabled {
		t.Fatalf("development keeps the switch: %v", err)
	}
}

// Production-да байланыс поштасы міндетті: құпиялық саясаты мен қолдау беті соған сілтейді.
func TestProductionRequiresContactEmail(t *testing.T) {
	setProductionEnv(t)
	unset(t, "CONTACT_EMAIL")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "CONTACT_EMAIL must be set when APP_ENV=production") {
		t.Fatalf("production without a contact: %v", err)
	}
	t.Setenv("CONTACT_EMAIL", "support@ai-reply.kz")
	if cfg, err := Load(""); err != nil || cfg.App.ContactEmail() != "support@ai-reply.kz" {
		t.Fatalf("production with a contact: %v", err)
	}
	setValidEnv(t)
	unset(t, "CONTACT_EMAIL")
	if _, err := Load(""); err != nil {
		t.Fatalf("development may leave it empty: %v", err)
	}
}

// Демо сатып алу тек development пен test-те; staging пен production-да іске қосылмайды.
func TestDemoCheckoutOnlyForDevelopmentAndTest(t *testing.T) {
	for env, ok := range map[string]bool{"development": true, "test": true, "staging": false, "production": false} {
		setValidEnv(t)
		t.Setenv("APP_ENV", env)
		t.Setenv("AUTH_DEMO_MODE", "false")
		t.Setenv("PAYMENT_MODE", "demo")
		t.Setenv("PAYMENT_DEMO_CHECKOUT", "true")
		t.Setenv("PUBLIC_BASE_URL", "https://ai-reply.kz")
		t.Setenv("RESEND_API_KEY", "re_test_key")
		t.Setenv("RESEND_FROM_EMAIL", "noreply@ai-reply.kz")
		_, err := Load("")
		if ok && err != nil {
			t.Errorf("%s: %v", env, err)
		}
		if !ok && (err == nil || !strings.Contains(err.Error(), "PAYMENT_DEMO_CHECKOUT=true is allowed only when APP_ENV is development or test")) {
			t.Errorf("%s accepted demo checkout (err %v)", env, err)
		}
	}
	// Staging may still run the demo provider without checkout.
	setValidEnv(t)
	t.Setenv("APP_ENV", "staging")
	t.Setenv("AUTH_DEMO_MODE", "false")
	t.Setenv("PAYMENT_MODE", "demo")
	unset(t, "PAYMENT_DEMO_CHECKOUT")
	if _, err := Load(""); err != nil {
		t.Fatalf("staging with PAYMENT_MODE=demo and no checkout: %v", err)
	}
}

// Белгісіз орта іске қосылмайды: қате жазылған APP_ENV production тексерулерін айналып өтпейді.
func TestAppEnvMustBeKnown(t *testing.T) {
	for _, env := range []string{"prod", "Production1", "live"} {
		setValidEnv(t)
		t.Setenv("APP_ENV", env)
		t.Setenv("AUTH_DEMO_MODE", "false")
		if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "APP_ENV must be development, test, staging or production") {
			t.Errorf("APP_ENV=%q accepted (err %v)", env, err)
		}
	}
	setProductionEnv(t)
	t.Setenv("APP_ENV", " Production ")
	if cfg, err := Load(""); err != nil || !cfg.App.IsProduction() {
		t.Fatalf("APP_ENV is trimmed and lowercased: %q %v", cfg.App.Env, err)
	}
}

// Ескертулер іске қосылуды тоқтатпайды: оператор аты production-да, Apple токенін кері қайтару кілті.
func TestStartupWarnings(t *testing.T) {
	setProductionEnv(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if w := strings.Join(cfg.Warnings(), "\n"); !strings.Contains(w, "LEGAL_OPERATOR_NAME is empty") || strings.Contains(w, "APPLE") {
		t.Fatalf("production without an operator: %q", w)
	}

	t.Setenv("LEGAL_OPERATOR_NAME", "Operator Name")
	t.Setenv("APPLE_CLIENT_ID", "kz.ai-reply.app")
	if cfg, err = Load(""); err != nil {
		t.Fatalf("apple sign-in without the revocation key still starts: %v", err)
	}
	if w := strings.Join(cfg.Warnings(), "\n"); strings.Contains(w, "LEGAL_OPERATOR_NAME") ||
		!strings.Contains(w, "APPLE_TEAM_ID, APPLE_KEY_ID and APPLE_PRIVATE_KEY are not set") || !strings.Contains(w, "5.1.1(v)") {
		t.Fatalf("apple without revocation: %q", w)
	}

	t.Setenv("APPLE_TEAM_ID", "TEAM123")
	t.Setenv("APPLE_KEY_ID", "KEY123")
	t.Setenv("APPLE_PRIVATE_KEY", `-----BEGIN PRIVATE KEY-----\nc2VjcmV0\n-----END PRIVATE KEY-----`)
	if cfg, err = Load(""); err != nil || len(cfg.Warnings()) != 0 {
		t.Fatalf("complete production setup: %v %v", cfg.Warnings(), err)
	}

	// Outside production an empty operator is not worth a warning.
	setValidEnv(t)
	unset(t, "LEGAL_OPERATOR_NAME", "APPLE_CLIENT_ID", "APPLE_TEAM_ID", "APPLE_KEY_ID", "APPLE_PRIVATE_KEY")
	if cfg, err = Load(""); err != nil || len(cfg.Warnings()) != 0 {
		t.Fatalf("development: %v %v", cfg.Warnings(), err)
	}
}

// APPLE_CLIENT_ID bundle id-ге ұқсамаса, іске қосылғанда ескерту шығады (Apple кіруі aud бойынша құлайды).
func TestAppleClientIDMustLookLikeABundleID(t *testing.T) {
	for id, ok := range map[string]bool{
		"kz.ai-reply.reply.keyboard.keyboard":            true,
		"kz.yerek.replykeyboard":                         true, // a bundle id, only not this app's: the per-token warning names it
		"123-ios.apps.googleusercontent.com":             false,
		"https://appleid.apple.com":                      false,
		"ABCDE12345.kz.ai-reply.reply.keyboard.keyboard": false,
		"aireply":         false,
		"kz.ai reply.app": false,
	} {
		setValidEnv(t)
		t.Setenv("APPLE_CLIENT_ID", id)
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("%q: %v", id, err)
		}
		warned := strings.Contains(strings.Join(cfg.Warnings(), "\n"), "does not look like the iOS app's bundle id")
		if warned == ok {
			t.Errorf("APPLE_CLIENT_ID=%q: warned=%v", id, warned)
		}
	}
}
