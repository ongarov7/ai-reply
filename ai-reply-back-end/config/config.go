// Package config — барлық баптау тек қоршаған ортадан оқылады, кодта құпия жоқ.
package config

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
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
	AI        AI
	Analytics Analytics
	Admin     Admin
	Payments  Payments
	Limits    Limits
	Log       Log
	Push      Push
	Legal     Legal
	Retention Retention
}

type App struct {
	Env           string // development | test | staging | production
	Port          int
	Host          string
	PublicBaseURL string
	Timezone      string
	location      *time.Location
	CORSOrigins   []string
	TrustProxy    bool
	Contact       string
	// SimulatorEnabled — /simulator өнім демонстрациясы (SIMULATOR_ENABLED; әдепкі тек development/test).
	SimulatorEnabled bool
	// AppStoreURL, PlayStoreURL — лендингтегі дүкен сілтемелері; бос болса «жақында» белгісі қалады.
	AppStoreURL  string
	PlayStoreURL string
}

// ContactEmail — қолдау мен заң беттеріндегі байланыс мекенжайы (CONTACT_EMAIL).
//
// Empty when it is not configured: pages and /api/v1/config then show no
// address at all rather than one nobody reads.
func (a App) ContactEmail() string { return a.Contact }

// IsProduction — өндірістік режим (demo мүмкіндіктері мұнда өшіріледі).
func (a App) IsProduction() bool { return a.Env == "production" }

// AllowsDemo — демо кіру тек жергілікті әзірлеу мен тестте рұқсат (staging те нақты сервер).
func (a App) AllowsDemo() bool { return a.Env == "development" || a.Env == "test" }

// HTTPS — сервер көпшілікке https арқылы ашылады (PUBLIC_BASE_URL).
func (a App) HTTPS() bool { return strings.HasPrefix(a.PublicBaseURL, "https://") }

// HSTS — Strict-Transport-Security тақырыбы жіберіле ме: production не https мекенжайы.
func (a App) HSTS() bool { return a.IsProduction() || a.HTTPS() }

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
	// Бір поштаның тәулік ішіндегі қате код енгізу шегі (барлық кодтарының әрекеттері қосылады).
	OTPMaxFailedPerDay int
	// ReviewEmail, ReviewCode — App Review / Play review тексерушісінің кіруі
	// (REVIEW_LOGIN_EMAIL, REVIEW_LOGIN_CODE). Тек осы поштаға код хатсыз
	// шығарылады, барлық шектер сақталады. Әр жіберілімге жаңа код, мақұлданған
	// соң екеуі де тазаланады.
	ReviewEmail string
	ReviewCode  string
}

// ReviewLogin — тексерушінің кіруі бапталған ба.
func (a Auth) ReviewLogin() bool { return a.ReviewEmail != "" && a.ReviewCode != "" }

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
	// Apple токендерін кері қайтару (тіркелгі жойылғанда): Apple Developer ▸
	// Keys ▸ «Sign in with Apple» кілті. Үшеуі бірге беріледі не мүлде берілмейді;
	// ApplePrivateKey — .p8 PEM ("\n" escapes and base64 work as for Firebase).
	AppleTeamID     string
	AppleKeyID      string
	ApplePrivateKey string
}

// AppleRevocation — тіркелгі жойылғанда Apple токенін кері қайтаруға кілт бар ма.
func (o OAuth) AppleRevocation() bool {
	return o.AppleTeamID != "" && o.AppleKeyID != "" && o.ApplePrivateKey != "" && len(o.AppleClientIDs) > 0
}

type OpenAI struct {
	APIKey          string
	Model           string
	BaseURL         string
	MaxOutputTokens int
	Timeout         time.Duration
	// Temperature — nil болса өріс сұранысқа мүлде қосылмайды: reasoning
	// модельдері оны қабылдамайды (OPENAI_TEMPERATURE=none).
	Temperature *float64
}

// AI — генерация сапасы мен қосымша AI мүмкіндіктері.
type AI struct {
	// RepairEnabled — тексеруден өтпеген жауапқа бір рет түзету сұранысы.
	RepairEnabled bool
	// PolishEnabled — POST /api/v1/ai/polish (нұсқауды түзету ұсынысы).
	PolishEnabled bool
}

// Analytics — қолданбалардың өнім оқиғалары.
type Analytics struct {
	// ProductEventsEnabled — POST /api/v1/analytics/events; өшірулі болса 404.
	ProductEventsEnabled bool
}

type Admin struct {
	BootstrapEmail    string
	BootstrapPassword string
	SessionTTL        time.Duration
	CookieName        string
	SecureCookies     bool
}

type Payments struct {
	Mode string // off | demo | live
	// DemoCheckout — the demo provider may "sell" plans (PAYMENT_MODE=demo on a
	// development server only). Without it no plan can be bought until a live
	// provider exists.
	DemoCheckout bool
}

// PaymentModes — PAYMENT_MODE мәндері. off — төлем мүлде жоқ (әдепкі: қолданбаларда
// StoreKit / Play Billing әлі жоқ), demo — әзірлеу, live — нақты провайдер.
var PaymentModes = []string{"off", "demo", "live"}

// Legal — заң беттеріндегі оператор (LEGAL_OPERATOR_NAME, LEGAL_OPERATOR_DETAILS).
//
// Both are optional and rendered as given. Empty means the pages name only the
// app and the contact address: nothing is ever made up in their place.
type Legal struct {
	OperatorName    string
	OperatorDetails string // мекенжай, тіркеу нөмірі
}

// Retention — ескі жолдарды тазалау мерзімдері (күнмен, 0 — өшірілмейді).
//
// Account data itself lives until the account is deleted; these windows are
// for rows that outlive their use: spent sign-in codes, dead refresh tokens,
// installations nobody signs in on, and metadata older than a year.
type Retention struct {
	OTPDays               int // otp_codes, жасалғаннан бастап
	RefreshTokenDays      int // refresh_tokens, мерзімі біткен не кері қайтарылғаннан бастап
	AnonInstallationsDays int // аккаунтсыз app_installations, соңғы көрінгеннен бастап
	ProductEventsDays     int // product_events
	AIUsageEventsDays     int // ai_usage_events
}

type Limits struct {
	SourceTextChars  int
	InstructionChars int
	RequestBodyBytes int64
	// PolishPerDay — бір қолданушының сервер күніндегі polish шегі (шығын шегі; біткенде ұсыныс жоқ).
	PolishPerDay int
	// OTPRequestPerHour, OTPVerifyPerHour — IP бойынша және әдейі кең: оператордың
	// NAT-ы артында көп адам бір мекенжайда. Нақты қорғаныс — поштаға қойылған шектер.
	OTPRequestPerHour int
	OTPVerifyPerHour  int
	AIPerMinute       int
	PolishPerMinute   int
	EventsPerMinute   int
	// ReportsPerHour — AI жауабына шағым: бір қолданушыға сағатына.
	ReportsPerHour    int
	AdminLoginPerHour int
	GenericPerMinute  int
}

type Log struct {
	Level  string
	Format string // json | text
}

// Push — FCM арқылы хабарлама жіберу (Android те, iOS та) және хабарлама хаттары.
//
// Provider credentials exist only here, read from the environment; the apps
// never see them. PUSH_NOTIFICATIONS_ENABLED=false (the default) keeps the
// server fully working without Firebase: installations are still registered,
// automatic events are recorded as skipped, nothing is sent.
type Push struct {
	Enabled          bool
	WorkerEnabled    bool
	MaxAttempts      int
	BatchSize        int
	Concurrency      int
	CampaignsPerHour int
	LinkHosts        []string
	// QuotaLowPercent — «квота аяқталуға жақын» шегі, лимиттің пайызы (күндік және айлық).
	QuotaLowPercent int
	// RetentionDays — аяқталған жеткізулер мен хабарламалар қанша күн сақталады (0 — өшірілмейді).
	RetentionDays int
	// EmailsEnabled — хабарлама хаттары (тек Resend бапталса жұмыс істейді).
	EmailsEnabled bool
	FCM           FCM
	// problems — FIREBASE_SERVICE_ACCOUNT_FILE оқылмады (Validate хабарлайды).
	problems []string
}

// FCM — Firebase Cloud Messaging HTTP v1 (қызметтік тіркелгі).
type FCM struct {
	ProjectID   string
	ClientEmail string
	PrivateKey  string // PEM; "\n" escapes and base64 in .env are turned into a normal key
	// ServiceAccountFile — the JSON file the three values came from ("" — from the variables).
	ServiceAccountFile string
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

// Load — .env файлын (бар болса) оқып, ортадан баптауды жинайды.
func Load(envFile string) (Config, error) {
	if envFile != "" {
		if err := loadDotEnv(envFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Config{}, err
		}
	}

	publicBaseURL := strings.TrimRight(str("PUBLIC_BASE_URL", "https://ai-reply.kz"), "/")
	appEnv := str("APP_ENV", "development")
	cfg := Config{
		App: App{
			Env:           appEnv,
			Port:          num("APP_PORT", 0), // әдепкі жоқ: Docker порт картасы мен healthcheck те осы мәнді қолданады
			Host:          str("APP_HOST", "0.0.0.0"),
			PublicBaseURL: publicBaseURL,
			Timezone:      str("DEFAULT_TIMEZONE", "Asia/Almaty"),
			CORSOrigins:   list("CORS_ORIGINS", ""),
			TrustProxy:    boolean("TRUST_PROXY", false),
			Contact:       strings.ToLower(str("CONTACT_EMAIL", "")),
			// The product demo signs in with the admin account and drives a
			// real AI account: on a public server it is opt-in.
			SimulatorEnabled: boolean("SIMULATOR_ENABLED", appEnv == "development" || appEnv == "test"),
			AppStoreURL:      str("APP_STORE_URL", ""),
			PlayStoreURL:     str("PLAY_STORE_URL", ""),
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
			DemoMode:           boolean("AUTH_DEMO_MODE", false),
			DemoOTP:            str("AUTH_DEMO_OTP", "1111"),
			OTPTTL:             dur("OTP_TTL", 5*time.Minute),
			OTPMaxAttempts:     num("OTP_MAX_ATTEMPTS", 5),
			OTPRequestsPerHour: num("RATE_OTP_REQUEST_PER_ADDRESS_PER_HOUR", 5), // бір поштаға; IP шегі бөлек
			OTPChannel:         str("OTP_CHANNEL", "stub"),
			LegacySecret:       str("AUTH_SIGNING_SECRET", ""),
			LegacyEnabled:      boolean("LEGACY_API_ENABLED", false),
			OTPResendCooldown:  dur("OTP_RESEND_COOLDOWN", 32*time.Second),
			OTPRequestsPerDay:  num("RATE_OTP_REQUEST_PER_DAY", 10),
			OTPMaxFailedPerDay: num("OTP_MAX_FAILED_PER_DAY", 10),
			ReviewEmail:        strings.ToLower(str("REVIEW_LOGIN_EMAIL", "")),
			ReviewCode:         str("REVIEW_LOGIN_CODE", ""),
		},
		Email: Email{
			ResendAPIKey: str("RESEND_API_KEY", ""),
			FromEmail:    strings.ToLower(str("RESEND_FROM_EMAIL", "")),
			FromName:     str("RESEND_FROM_NAME", "AI Reply"),
		},
		OAuth: OAuth{
			GoogleClientIDs: append(list("GOOGLE_CLIENT_ID_IOS", ""), list("GOOGLE_CLIENT_ID_WEB", "")...),
			AppleClientIDs:  list("APPLE_CLIENT_ID", ""),
			AppleTeamID:     str("APPLE_TEAM_ID", ""),
			AppleKeyID:      str("APPLE_KEY_ID", ""),
			ApplePrivateKey: PEM(str("APPLE_PRIVATE_KEY", "")),
		},
		OpenAI: OpenAIFromEnv(),
		AI: AI{
			RepairEnabled: boolean("AI_REPAIR_ENABLED", true),
			PolishEnabled: boolean("AI_POLISH_ENABLED", true),
		},
		Analytics: Analytics{ProductEventsEnabled: boolean("PRODUCT_EVENTS_ENABLED", true)},
		Admin: Admin{
			BootstrapEmail:    strings.ToLower(strings.TrimSpace(str("ADMIN_EMAIL", ""))),
			BootstrapPassword: str("ADMIN_PASSWORD", ""),
			SessionTTL:        dur("ADMIN_SESSION_TTL", 8*time.Hour),
			CookieName:        str("ADMIN_COOKIE_NAME", "aireply_admin"),
			// Secure whenever the site is served over https; an explicit value wins.
			SecureCookies: boolean("ADMIN_SECURE_COOKIES",
				appEnv == "production" || strings.HasPrefix(publicBaseURL, "https://")),
		},
		Payments: Payments{
			Mode:         strings.ToLower(str("PAYMENT_MODE", "off")),
			DemoCheckout: boolean("PAYMENT_DEMO_CHECKOUT", false),
		},
		Limits: Limits{
			SourceTextChars:   num("LIMIT_SOURCE_TEXT_CHARS", 400),
			InstructionChars:  num("LIMIT_INSTRUCTION_CHARS", 400),
			RequestBodyBytes:  int64(num("LIMIT_REQUEST_BODY_BYTES", 32*1024)),
			PolishPerDay:      num("LIMIT_POLISH_PER_DAY", 100),
			OTPRequestPerHour: num("RATE_OTP_REQUEST_PER_HOUR", 30),
			OTPVerifyPerHour:  num("RATE_OTP_VERIFY_PER_HOUR", 60),
			AIPerMinute:       num("RATE_AI_PER_MINUTE", 12),
			PolishPerMinute:   num("RATE_POLISH_PER_MINUTE", 20),
			EventsPerMinute:   num("RATE_EVENTS_PER_MINUTE", 30),
			ReportsPerHour:    num("RATE_AI_REPORTS_PER_HOUR", 20),
			AdminLoginPerHour: num("RATE_ADMIN_LOGIN_PER_HOUR", 10),
			GenericPerMinute:  num("RATE_GENERIC_PER_MINUTE", 60),
		},
		Log:  Log{Level: str("LOG_LEVEL", "info"), Format: str("LOG_FORMAT", "json")},
		Push: pushFromEnv(),
		Legal: Legal{
			OperatorName:    str("LEGAL_OPERATOR_NAME", ""),
			OperatorDetails: str("LEGAL_OPERATOR_DETAILS", ""),
		},
		Retention: Retention{
			OTPDays:               days("RETENTION_OTP_DAYS", 30),
			RefreshTokenDays:      days("RETENTION_REFRESH_TOKENS_DAYS", 30),
			AnonInstallationsDays: days("RETENTION_ANON_INSTALLATIONS_DAYS", 180),
			ProductEventsDays:     days("RETENTION_PRODUCT_EVENTS_DAYS", 400),
			AIUsageEventsDays:     days("RETENTION_AI_USAGE_EVENTS_DAYS", 400),
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

// OpenAIFromEnv — провайдер баптауы. Load те, сапа бағалауының тірі тесті де
// (internal/ai/eval) осыны оқиды: әдепкі мәндердің бір ғана көзі бар.
func OpenAIFromEnv() OpenAI {
	return OpenAI{
		APIKey:          str("OPENAI_API_KEY", ""),
		Model:           str("OPENAI_MODEL", "gpt-4o-mini"),
		BaseURL:         strings.TrimRight(str("OPENAI_BASE_URL", "https://api.openai.com/v1"), "/"),
		MaxOutputTokens: num("OPENAI_MAX_OUTPUT_TOKENS", 180),
		Timeout:         dur("OPENAI_TIMEOUT", 20*time.Second),
		Temperature:     temperature("OPENAI_TEMPERATURE", 0.7),
	}
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
	if c.Auth.LegacyEnabled && (c.Auth.LegacySecret == c.Auth.AccessSecret || c.Auth.LegacySecret == c.Auth.RefreshSecret) {
		problems = append(problems, "AUTH_SIGNING_SECRET must differ from JWT_ACCESS_SECRET and JWT_REFRESH_SECRET")
	}
	problems = append(problems, c.placeholders()...)
	// Демо OTP (тұрақты код, жалған SMS) тек жергілікті әзірлеу мен тестте:
	// staging те нақты адамдар кіретін сервер.
	if c.Auth.DemoMode && !c.App.AllowsDemo() {
		problems = append(problems, "AUTH_DEMO_MODE=true is allowed only when APP_ENV is development or test")
	}
	if c.App.IsProduction() {
		if c.Payments.Mode == "demo" {
			problems = append(problems, "PAYMENT_MODE=demo is not allowed when APP_ENV=production")
		}
		if c.Payments.DemoCheckout {
			problems = append(problems, "PAYMENT_DEMO_CHECKOUT must be false when APP_ENV=production")
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
	if c.App.Contact != "" {
		// Shown as a mailto link on the support, privacy and deletion pages and
		// sent to the apps: it has to be a real, plain address.
		if addr, err := mail.ParseAddress(c.App.Contact); err != nil || addr.Name != "" || addr.Address != c.App.Contact {
			problems = append(problems, "CONTACT_EMAIL must be a plain address of a mailbox someone reads, e.g. support@ai-reply.kz")
		}
	}
	if c.App.IsProduction() && !c.Email.Enabled() {
		// Sign-in codes go out by e-mail; production without a provider would
		// accept sign-in requests it can never deliver.
		problems = append(problems, "RESEND_API_KEY and RESEND_FROM_EMAIL must be set when APP_ENV=production")
	}
	if !contains(PaymentModes, c.Payments.Mode) {
		problems = append(problems, "PAYMENT_MODE must be off, demo or live")
	}
	if c.Payments.DemoCheckout && c.Payments.Mode != "demo" {
		problems = append(problems, "PAYMENT_DEMO_CHECKOUT=true needs PAYMENT_MODE=demo")
	}
	if set := countSet(c.OAuth.AppleTeamID, c.OAuth.AppleKeyID, c.OAuth.ApplePrivateKey); set > 0 && set < 3 {
		problems = append(problems, "APPLE_TEAM_ID, APPLE_KEY_ID and APPLE_PRIVATE_KEY must be set together")
	} else if set == 3 && len(c.OAuth.AppleClientIDs) == 0 {
		problems = append(problems, "APPLE_CLIENT_ID must be set to revoke Apple tokens")
	}
	if c.Admin.BootstrapEmail != "" {
		switch {
		case c.App.IsProduction() && len(c.Admin.BootstrapPassword) < 16:
			problems = append(problems, "ADMIN_PASSWORD must be at least 16 characters when APP_ENV=production")
		case len(c.Admin.BootstrapPassword) < 10:
			problems = append(problems, "ADMIN_PASSWORD must be at least 10 characters")
		}
	}
	problems = append(problems, c.Auth.validateReviewLogin()...)
	for _, store := range []struct{ name, value, prefix string }{
		{"APP_STORE_URL", c.App.AppStoreURL, "https://apps.apple.com/"},
		{"PLAY_STORE_URL", c.App.PlayStoreURL, "https://play.google.com/"},
	} {
		if store.value != "" && (!strings.HasPrefix(store.value, store.prefix) || strings.ContainsAny(store.value, " \"<>")) {
			problems = append(problems, store.name+" must be the app's page, starting with "+store.prefix)
		}
	}
	problems = append(problems, c.Push.validate()...)
	return problems
}

// placeholders — .env.example-дегі толтырғыштар (REPLACE…, CHANGE_ME) қалып қойған құпиялар.
//
// A copied example file must not start a server whose tokens anyone can sign
// with the value printed in the repository. Only values in use are checked:
// the legacy secret while the legacy API is on, the admin password while an
// admin is bootstrapped, the Resend key when one is set.
func (c Config) placeholders() []string {
	type secret struct {
		name, value string
		used        bool
	}
	var problems []string
	for _, s := range []secret{
		{"OPENAI_API_KEY", c.OpenAI.APIKey, true},
		{"JWT_ACCESS_SECRET", c.Auth.AccessSecret, true},
		{"JWT_REFRESH_SECRET", c.Auth.RefreshSecret, true},
		{"AUTH_SIGNING_SECRET", c.Auth.LegacySecret, c.Auth.LegacyEnabled},
		{"ADMIN_PASSWORD", c.Admin.BootstrapPassword, c.Admin.BootstrapEmail != ""},
		{"RESEND_API_KEY", c.Email.ResendAPIKey, true},
	} {
		upper := strings.ToUpper(s.value)
		if s.used && (strings.Contains(upper, "REPLACE") || strings.Contains(upper, "CHANGE_ME")) {
			problems = append(problems, s.name+" still holds the placeholder from .env.example; set a real value")
		}
	}
	return problems
}

// validateReviewLogin — REVIEW_LOGIN_EMAIL мен REVIEW_LOGIN_CODE бірге: бір пошта және 4 цифр.
func (a Auth) validateReviewLogin() []string {
	switch countSet(a.ReviewEmail, a.ReviewCode) {
	case 0:
		return nil
	case 1:
		return []string{"REVIEW_LOGIN_EMAIL and REVIEW_LOGIN_CODE must be set together"}
	}
	var problems []string
	if addr, err := mail.ParseAddress(a.ReviewEmail); err != nil || addr.Name != "" || addr.Address != a.ReviewEmail ||
		!strings.Contains(a.ReviewEmail[strings.LastIndexByte(a.ReviewEmail, '@')+1:], ".") {
		problems = append(problems, "REVIEW_LOGIN_EMAIL must be exactly one plain address")
	}
	if !isDigits(a.ReviewCode, 4) {
		problems = append(problems, "REVIEW_LOGIN_CODE must be exactly 4 digits")
	}
	return problems
}

func pushFromEnv() Push {
	p := Push{
		Enabled:          boolean("PUSH_NOTIFICATIONS_ENABLED", false),
		WorkerEnabled:    boolean("PUSH_WORKER_ENABLED", true),
		MaxAttempts:      num("PUSH_MAX_ATTEMPTS", 5),
		BatchSize:        num("PUSH_BATCH_SIZE", 50),
		Concurrency:      num("PUSH_WORKER_CONCURRENCY", 8),
		CampaignsPerHour: num("RATE_PUSH_CAMPAIGNS_PER_HOUR", 10),
		LinkHosts:        list("PUSH_LINK_HOSTS", hostOf(str("PUBLIC_BASE_URL", "https://ai-reply.kz"))),
		QuotaLowPercent:  num("PUSH_QUOTA_LOW_PERCENT", 10),
		RetentionDays:    days("RETENTION_NOTIFICATIONS_DAYS", 180),
		EmailsEnabled:    boolean("NOTIFICATION_EMAILS_ENABLED", true),
		FCM: FCM{
			ProjectID:   str("FIREBASE_PROJECT_ID", ""),
			ClientEmail: str("FIREBASE_CLIENT_EMAIL", ""),
			PrivateKey:  PEM(str("FIREBASE_PRIVATE_KEY", "")),
		},
	}
	if path := str("FIREBASE_SERVICE_ACCOUNT_FILE", ""); path != "" {
		if p.FCM.ProjectID != "" || p.FCM.ClientEmail != "" || p.FCM.PrivateKey != "" {
			p.problems = append(p.problems,
				"set either FIREBASE_SERVICE_ACCOUNT_FILE or FIREBASE_PROJECT_ID, FIREBASE_CLIENT_EMAIL and FIREBASE_PRIVATE_KEY, not both")
			return p
		}
		fcm, err := readServiceAccount(path)
		if err != nil {
			p.problems = append(p.problems, err.Error())
			return p
		}
		p.FCM = fcm
	}
	return p
}

// readServiceAccount — Firebase Console жүктеп берген JSON файлы. Қате мәтінінде
// файлдың мазмұны (кілт) ешқашан болмайды.
func readServiceAccount(path string) (FCM, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return FCM{}, fmt.Errorf("FIREBASE_SERVICE_ACCOUNT_FILE %q cannot be read", path)
	}
	var account struct {
		Type        string `json:"type"`
		ProjectID   string `json:"project_id"`
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
	}
	if json.Unmarshal(raw, &account) != nil || account.ProjectID == "" || account.ClientEmail == "" ||
		account.PrivateKey == "" || (account.Type != "" && account.Type != "service_account") {
		return FCM{}, fmt.Errorf("FIREBASE_SERVICE_ACCOUNT_FILE %q is not a Firebase service-account JSON file", path)
	}
	return FCM{
		ProjectID:          strings.TrimSpace(account.ProjectID),
		ClientEmail:        strings.TrimSpace(account.ClientEmail),
		PrivateKey:         PEM(account.PrivateKey),
		ServiceAccountFile: path,
	}, nil
}

// validate — push баптауы. Толық бос провайдер жай ғана өшірулі; жартылай
// толтырылғаны — қате (оператор бірдеңені ұмытқан).
func (p Push) validate() []string {
	problems := append([]string(nil), p.problems...)
	if p.FCM.partial() {
		problems = append(problems, "FIREBASE_PROJECT_ID, FIREBASE_CLIENT_EMAIL and FIREBASE_PRIVATE_KEY must be set together")
	}
	if p.MaxAttempts < 1 || p.MaxAttempts > 10 {
		problems = append(problems, "PUSH_MAX_ATTEMPTS must be between 1 and 10")
	}
	if p.QuotaLowPercent < 1 || p.QuotaLowPercent > 100 {
		problems = append(problems, "PUSH_QUOTA_LOW_PERCENT must be between 1 and 100")
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

// temperature — орнатылмаса әдепкі мән; бос не "none" болса nil (өріс
// жіберілмейді). Танылмаған мән әдепкіге түседі, үнсіз өзгеріс болмасын.
func temperature(key string, fallback float64) *float64 {
	// An empty value keeps the default, exactly as before; only an explicit
	// "none" leaves the field out.
	raw := strings.ToLower(str(key, ""))
	if raw == "none" {
		return nil
	}
	if v, err := strconv.ParseFloat(raw, 64); err == nil {
		return &v
	}
	return &fallback
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

// countSet — бос емес мәндер саны (бірге берілуі тиіс баптаулар үшін).
func countSet(values ...string) int {
	n := 0
	for _, v := range values {
		if v != "" {
			n++
		}
	}
	return n
}

func contains(all []string, v string) bool {
	for _, a := range all {
		if a == v {
			return true
		}
	}
	return false
}
