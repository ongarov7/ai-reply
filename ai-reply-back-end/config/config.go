// Package config — барлық баптау тек қоршаған ортадан оқылады, кодта құпия жоқ.
package config

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config — қолданбаның толық баптауы.
type Config struct {
	App       App
	Database  Database
	Auth      Auth
	Email     Email
	OAuth     OAuth
	OpenAI    OpenAI
	Admin     Admin
	Payments  Payments
	Limits    Limits
	Log       Log
	Push      Push
	Telemetry Telemetry
}

type App struct {
	Env           string // development | staging | production
	Port          int
	Host          string
	PublicBaseURL string
	Timezone      string
	location      *time.Location
	CORSOrigins   []string
	TrustProxy    bool
	Contact       string
}

// ContactEmail — лендингтегі байланыс мекенжайы.
func (a App) ContactEmail() string {
	if a.Contact != "" {
		return a.Contact
	}
	return "hello@aireply.app"
}

// IsProduction — өндірістік режим (demo мүмкіндіктері мұнда өшіріледі).
func (a App) IsProduction() bool { return a.Env == "production" }

// Location — күндік квота қай белдеу бойынша жаңаратынын анықтайды.
func (a App) Location() *time.Location {
	if a.location != nil {
		return a.location
	}
	return time.UTC
}

type Database struct {
	Path           string
	MaxReadConns   int
	BusyTimeout    time.Duration
	MigrateOnStart bool
}

type Auth struct {
	AccessSecret   string
	RefreshSecret  string
	AccessTTL      time.Duration
	RefreshTTL     time.Duration
	Issuer         string
	DemoMode       bool
	DemoOTP        string
	OTPTTL         time.Duration
	OTPMaxAttempts int
	// Бір идентификаторға сағатына сұрауға болатын код саны.
	OTPRequestsPerHour int
	OTPChannel         string // stub | sms | whatsapp | email
	LegacySecret       string // ескі мобильді build-тердің install-token қолтаңбасы
	LegacyEnabled      bool
	// Бір поштаға жаңа кодты қайта сұрауға болатын ең аз аралық (сервер тексереді).
	OTPResendCooldown time.Duration
	// Бір поштаға тәулігіне сұрауға болатын код саны.
	OTPRequestsPerDay int
}

// Email — транзакциялық хаттар (Resend). Кілт тек ортадан келеді.
type Email struct {
	ResendAPIKey string
	FromEmail    string
	FromName     string
}

// Enabled — нақты хат жеткізу бапталған ба.
func (e Email) Enabled() bool { return e.ResendAPIKey != "" && e.FromEmail != "" }

// OAuth — Google және Apple арқылы кіру.
//
// Each list holds the values a provider's token may carry in `aud`: the iOS
// OAuth client id and the Web client id Android passes to Credential Manager
// for Google, the app's bundle id for Apple. An empty list switches that
// provider off; nothing here is secret.
type OAuth struct {
	GoogleClientIDs []string
	AppleClientIDs  []string
}

type OpenAI struct {
	APIKey          string
	Model           string
	BaseURL         string
	MaxOutputTokens int
	Timeout         time.Duration
	Temperature     float64
}

type Admin struct {
	BootstrapEmail    string
	BootstrapPassword string
	SessionTTL        time.Duration
	CookieName        string
	SecureCookies     bool
}

type Payments struct {
	Mode string // demo | live
}

type Limits struct {
	SourceTextChars   int
	InstructionChars  int
	RequestBodyBytes  int64
	OTPRequestPerHour int
	OTPVerifyPerHour  int
	AIPerMinute       int
	AdminLoginPerHour int
	GenericPerMinute  int
}

type Log struct {
	Level  string
	Format string // json | text
}

// Push — FCM (Android) мен APNs (iOS) арқылы хабарлама жіберу.
//
// Provider credentials exist only here, read from the environment; the apps
// never see them. PUSH_NOTIFICATIONS_ENABLED=false (the default) keeps the
// server fully working without any provider: installations are still
// registered, nothing is sent.
type Push struct {
	Enabled          bool
	WorkerEnabled    bool
	MaxAttempts      int
	BatchSize        int
	Concurrency      int
	CampaignsPerHour int
	LinkHosts        []string
	FCM              FCM
	APNs             APNs
}

// FCM — Firebase Cloud Messaging HTTP v1 (қызметтік тіркелгі).
type FCM struct {
	ProjectID   string
	ClientEmail string
	PrivateKey  string // PEM; "\n" escapes in .env are turned into newlines
}

// Configured — үш мәннің бәрі берілген.
func (f FCM) Configured() bool {
	return f.ProjectID != "" && f.ClientEmail != "" && f.PrivateKey != ""
}

// partial — бірі берілген, бірі жоқ (қате баптау).
func (f FCM) partial() bool {
	set := 0
	for _, v := range []string{f.ProjectID, f.ClientEmail, f.PrivateKey} {
		if v != "" {
			set++
		}
	}
	return set > 0 && set < 3
}

// APNs — Apple Push Notification service, токен (.p8) арқылы.
type APNs struct {
	KeyID       string
	TeamID      string
	BundleID    string
	PrivateKey  string // .p8 content (PEM)
	Environment string // production | sandbox: for tokens whose build did not say
}

// Configured — кілт, команда, bundle id және кілт мазмұны берілген.
func (a APNs) Configured() bool {
	return a.KeyID != "" && a.TeamID != "" && a.BundleID != "" && a.PrivateKey != ""
}

func (a APNs) partial() bool {
	set := 0
	for _, v := range []string{a.KeyID, a.TeamID, a.PrivateKey} {
		if v != "" {
			set++
		}
	}
	return set > 0 && set < 3
}

// Telemetry — қосымша оқиғалары және сақтау мерзімдері (күн; 0 — өшірмеу).
type Telemetry struct {
	Enabled                  bool
	EventsPerMinute          int
	RetentionAppEventsDays   int
	RetentionAPIErrorsDays   int
	RetentionAuthEventsDays  int
	RetentionDeliveriesDays  int
	RetentionAuditLogDays    int
	RecordClientErrorsStatus int // record API errors from this status up (4xx noise below is skipped)
}

// Load — .env файлын (бар болса) оқып, ортадан баптауды жинайды.
func Load(envFile string) (Config, error) {
	if envFile != "" {
		if err := loadDotEnv(envFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Config{}, err
		}
	}

	cfg := Config{
		App: App{
			Env:           str("APP_ENV", "development"),
			Port:          num("APP_PORT", 0), // әдепкі жоқ: Docker порт картасы мен healthcheck те осы мәнді қолданады
			Host:          str("APP_HOST", "0.0.0.0"),
			PublicBaseURL: strings.TrimRight(str("PUBLIC_BASE_URL", "https://ai-reply.kz"), "/"),
			Timezone:      str("DEFAULT_TIMEZONE", "Asia/Almaty"),
			CORSOrigins:   list("CORS_ORIGINS", ""),
			TrustProxy:    boolean("TRUST_PROXY", false),
			Contact:       str("CONTACT_EMAIL", ""),
		},
		Database: Database{
			Path:           str("SQLITE_PATH", "data/aireply.db"),
			MaxReadConns:   num("SQLITE_MAX_READ_CONNS", 8),
			BusyTimeout:    dur("SQLITE_BUSY_TIMEOUT", 5*time.Second),
			MigrateOnStart: boolean("DB_MIGRATE_ON_START", true),
		},
		Auth: Auth{
			AccessSecret:       str("JWT_ACCESS_SECRET", ""),
			RefreshSecret:      str("JWT_REFRESH_SECRET", ""),
			AccessTTL:          dur("ACCESS_TOKEN_TTL", 15*time.Minute),
			RefreshTTL:         dur("REFRESH_TOKEN_TTL", 30*24*time.Hour),
			Issuer:             str("JWT_ISSUER", "ai-reply"),
			DemoMode:           boolean("AUTH_DEMO_MODE", true),
			DemoOTP:            str("AUTH_DEMO_OTP", "1111"),
			OTPTTL:             dur("OTP_TTL", 5*time.Minute),
			OTPMaxAttempts:     num("OTP_MAX_ATTEMPTS", 5),
			OTPRequestsPerHour: num("RATE_OTP_REQUEST_PER_HOUR", 5),
			OTPChannel:         str("OTP_CHANNEL", "stub"),
			LegacySecret:       str("AUTH_SIGNING_SECRET", ""),
			LegacyEnabled:      boolean("LEGACY_API_ENABLED", true),
			OTPResendCooldown:  dur("OTP_RESEND_COOLDOWN", 32*time.Second),
			OTPRequestsPerDay:  num("RATE_OTP_REQUEST_PER_DAY", 10),
		},
		Email: Email{
			ResendAPIKey: str("RESEND_API_KEY", ""),
			FromEmail:    strings.ToLower(str("RESEND_FROM_EMAIL", "")),
			FromName:     str("RESEND_FROM_NAME", "AI Reply"),
		},
		OAuth: OAuth{
			GoogleClientIDs: append(list("GOOGLE_CLIENT_ID_IOS", ""), list("GOOGLE_CLIENT_ID_WEB", "")...),
			AppleClientIDs:  list("APPLE_CLIENT_ID", ""),
		},
		OpenAI: OpenAI{
			APIKey:          str("OPENAI_API_KEY", ""),
			Model:           str("OPENAI_MODEL", "gpt-4o-mini"),
			BaseURL:         strings.TrimRight(str("OPENAI_BASE_URL", "https://api.openai.com/v1"), "/"),
			MaxOutputTokens: num("OPENAI_MAX_OUTPUT_TOKENS", 180),
			Timeout:         dur("OPENAI_TIMEOUT", 20*time.Second),
			Temperature:     flt("OPENAI_TEMPERATURE", 0.7),
		},
		Admin: Admin{
			BootstrapEmail:    strings.ToLower(strings.TrimSpace(str("ADMIN_EMAIL", ""))),
			BootstrapPassword: str("ADMIN_PASSWORD", ""),
			SessionTTL:        dur("ADMIN_SESSION_TTL", 8*time.Hour),
			CookieName:        str("ADMIN_COOKIE_NAME", "aireply_admin"),
			SecureCookies:     boolean("ADMIN_SECURE_COOKIES", str("APP_ENV", "development") == "production"),
		},
		Payments: Payments{Mode: str("PAYMENT_MODE", "demo")},
		Limits: Limits{
			SourceTextChars:   num("LIMIT_SOURCE_TEXT_CHARS", 400),
			InstructionChars:  num("LIMIT_INSTRUCTION_CHARS", 400),
			RequestBodyBytes:  int64(num("LIMIT_REQUEST_BODY_BYTES", 32*1024)),
			OTPRequestPerHour: num("RATE_OTP_REQUEST_PER_HOUR", 5),
			OTPVerifyPerHour:  num("RATE_OTP_VERIFY_PER_HOUR", 10),
			AIPerMinute:       num("RATE_AI_PER_MINUTE", 12),
			AdminLoginPerHour: num("RATE_ADMIN_LOGIN_PER_HOUR", 10),
			GenericPerMinute:  num("RATE_GENERIC_PER_MINUTE", 60),
		},
		Log: Log{Level: str("LOG_LEVEL", "info"), Format: str("LOG_FORMAT", "json")},
		Push: Push{
			Enabled:          boolean("PUSH_NOTIFICATIONS_ENABLED", false),
			WorkerEnabled:    boolean("PUSH_WORKER_ENABLED", true),
			MaxAttempts:      num("PUSH_MAX_ATTEMPTS", 5),
			BatchSize:        num("PUSH_BATCH_SIZE", 50),
			Concurrency:      num("PUSH_WORKER_CONCURRENCY", 8),
			CampaignsPerHour: num("RATE_PUSH_CAMPAIGNS_PER_HOUR", 10),
			LinkHosts:        list("PUSH_LINK_HOSTS", hostOf(str("PUBLIC_BASE_URL", "https://ai-reply.kz"))),
			FCM: FCM{
				ProjectID:   str("FIREBASE_PROJECT_ID", ""),
				ClientEmail: str("FIREBASE_CLIENT_EMAIL", ""),
				PrivateKey:  PEM(str("FIREBASE_PRIVATE_KEY", "")),
			},
			APNs: APNs{
				KeyID:       str("APNS_KEY_ID", ""),
				TeamID:      str("APNS_TEAM_ID", ""),
				BundleID:    str("APNS_BUNDLE_ID", ""),
				PrivateKey:  PEM(str("APNS_PRIVATE_KEY", "")),
				Environment: apnsEnvironment(str("APNS_ENVIRONMENT", "production")),
			},
		},
		Telemetry: Telemetry{
			Enabled:                 boolean("TELEMETRY_ENABLED", true),
			EventsPerMinute:         num("RATE_EVENTS_PER_MINUTE", 30),
			RetentionAppEventsDays:  days("RETENTION_APP_EVENTS_DAYS", 90),
			RetentionAPIErrorsDays:  days("RETENTION_API_ERRORS_DAYS", 30),
			RetentionAuthEventsDays: days("RETENTION_AUTH_EVENTS_DAYS", 365),
			RetentionDeliveriesDays: days("RETENTION_NOTIFICATIONS_DAYS", 180),
			RetentionAuditLogDays:   days("RETENTION_AUDIT_LOG_DAYS", 0),
		},
	}

	loc, err := time.LoadLocation(cfg.App.Timezone)
	if err != nil {
		return Config{}, fmt.Errorf("DEFAULT_TIMEZONE %q: %w", cfg.App.Timezone, err)
	}
	cfg.App.location = loc

	if problems := cfg.Validate(); len(problems) > 0 {
		return Config{}, fmt.Errorf("configuration is incomplete:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return cfg, nil
}

// Validate — іске қосылу алдындағы қатаң тексеру: құпиясыз сервер көтерілмейді.
func (c Config) Validate() []string {
	var problems []string

	if c.App.Port < 1 || c.App.Port > 65535 {
		problems = append(problems, "APP_PORT must be set to a port number between 1 and 65535")
	}
	if c.OpenAI.APIKey == "" {
		problems = append(problems, "OPENAI_API_KEY is not set (the provider key lives only here)")
	}
	if len(c.Auth.AccessSecret) < 32 {
		problems = append(problems, "JWT_ACCESS_SECRET must be at least 32 characters")
	}
	if len(c.Auth.RefreshSecret) < 32 {
		problems = append(problems, "JWT_REFRESH_SECRET must be at least 32 characters")
	}
	if c.Auth.AccessSecret == c.Auth.RefreshSecret {
		problems = append(problems, "JWT_ACCESS_SECRET and JWT_REFRESH_SECRET must differ")
	}
	if c.Auth.LegacyEnabled && len(c.Auth.LegacySecret) < 32 {
		problems = append(problems, "AUTH_SIGNING_SECRET must be at least 32 characters while LEGACY_API_ENABLED=true")
	}
	if c.App.IsProduction() {
		// Демо OTP өндірісте автоматты түрде тыйым салынады.
		if c.Auth.DemoMode {
			problems = append(problems, "AUTH_DEMO_MODE must be false when APP_ENV=production")
		}
		if c.Payments.Mode == "demo" {
			problems = append(problems, "PAYMENT_MODE=demo is not allowed when APP_ENV=production")
		}
		if strings.HasPrefix(c.App.PublicBaseURL, "http://") {
			problems = append(problems, "PUBLIC_BASE_URL must be https in production")
		}
	}
	if c.Auth.DemoMode && !isDigits(c.Auth.DemoOTP, 4) {
		problems = append(problems, "AUTH_DEMO_OTP must be exactly 4 digits")
	}
	if c.Email.ResendAPIKey != "" || c.Email.FromEmail != "" {
		if addr, err := mail.ParseAddress(c.Email.FromEmail); err != nil || addr.Name != "" || addr.Address != c.Email.FromEmail {
			problems = append(problems, "RESEND_FROM_EMAIL must be a plain address on a domain verified in Resend, e.g. noreply@ai-reply.kz")
		}
	}
	if c.App.IsProduction() && !c.Email.Enabled() {
		// Sign-in codes go out by e-mail; production without a provider would
		// accept sign-in requests it can never deliver.
		problems = append(problems, "RESEND_API_KEY and RESEND_FROM_EMAIL must be set when APP_ENV=production")
	}
	if !contains([]string{"demo", "live"}, c.Payments.Mode) {
		problems = append(problems, "PAYMENT_MODE must be demo or live")
	}
	if c.Admin.BootstrapEmail != "" && len(c.Admin.BootstrapPassword) < 10 {
		problems = append(problems, "ADMIN_PASSWORD must be at least 10 characters")
	}
	problems = append(problems, c.Push.validate()...)
	return problems
}

// validate — push баптауы. Толық бос провайдер жай ғана өшірулі; жартылай
// толтырылғаны — қате (оператор бірдеңені ұмытқан).
func (p Push) validate() []string {
	var problems []string
	if p.FCM.partial() {
		problems = append(problems, "FIREBASE_PROJECT_ID, FIREBASE_CLIENT_EMAIL and FIREBASE_PRIVATE_KEY must be set together")
	}
	if p.APNs.partial() {
		problems = append(problems, "APNS_KEY_ID, APNS_TEAM_ID and APNS_PRIVATE_KEY must be set together")
	}
	if p.APNs.PrivateKey != "" && p.APNs.BundleID == "" {
		problems = append(problems, "APNS_BUNDLE_ID must be set when APNs is configured (the app's bundle id, the apns-topic)")
	}
	if p.APNs.Environment == "" {
		problems = append(problems, "APNS_ENVIRONMENT must be production or development")
	}
	if p.MaxAttempts < 1 || p.MaxAttempts > 10 {
		problems = append(problems, "PUSH_MAX_ATTEMPTS must be between 1 and 10")
	}
	return problems
}

// PEM — .env-тегі бір жолды кілтті PEM-ге айналдырады.
//
// A .env line cannot hold newlines, so keys arrive as "-----BEGIN ...-----\n
// MIIE...\n-----END ...-----" (exactly how the Firebase service-account JSON
// stores private_key) or base64 of the whole PEM file. Both become a normal
// multi-line PEM. The value is never logged.
func PEM(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "-----BEGIN") {
		if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && strings.Contains(string(decoded), "-----BEGIN") {
			raw = string(decoded)
		}
	}
	raw = strings.ReplaceAll(raw, `\r\n`, "\n")
	raw = strings.ReplaceAll(raw, `\n`, "\n")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	return strings.TrimSpace(raw) + "\n"
}

func apnsEnvironment(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "production", "prod":
		return "production"
	case "development", "sandbox", "dev":
		return "sandbox"
	default:
		return ""
	}
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// days — сақтау мерзімі күнмен; 0 рұқсат (өшірмеу дегенді білдіреді).
func days(key string, fallback int) int {
	raw := str(key, "")
	if raw == "" {
		return fallback
	}
	if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
		return v
	}
	return fallback
}

// ---------------------------------------------------------------- env helpers

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 1 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		value = strings.Trim(value, `"'`)
		if _, exists := os.LookupEnv(key); exists {
			continue // нақты орта әрқашан .env-тен басым
		}
		_ = os.Setenv(key, value)
	}
	return scanner.Err()
}

func str(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func num(key string, fallback int) int {
	if v, err := strconv.Atoi(str(key, "")); err == nil && v > 0 {
		return v
	}
	return fallback
}

func flt(key string, fallback float64) float64 {
	if v, err := strconv.ParseFloat(str(key, ""), 64); err == nil {
		return v
	}
	return fallback
}

func boolean(key string, fallback bool) bool {
	if v, err := strconv.ParseBool(str(key, "")); err == nil {
		return v
	}
	return fallback
}

func dur(key string, fallback time.Duration) time.Duration {
	raw := str(key, "")
	if raw == "" {
		return fallback
	}
	if v, err := time.ParseDuration(raw); err == nil && v > 0 {
		return v
	}
	if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return fallback
}

func list(key, fallback string) []string {
	raw := str(key, fallback)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isDigits(v string, length int) bool {
	if len(v) != length {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func contains(all []string, v string) bool {
	for _, a := range all {
		if a == v {
			return true
		}
	}
	return false
}
