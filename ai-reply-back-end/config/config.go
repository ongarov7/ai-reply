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
	Mode string // demo | live
	// DemoCheckout — the demo provider may "sell" plans (development and
	// tests only). Without it no plan can be bought until a live provider exists.
	DemoCheckout bool
}

type Limits struct {
	SourceTextChars   int
	InstructionChars  int
	RequestBodyBytes  int64
	OTPRequestPerHour int
	OTPVerifyPerHour  int
	AIPerMinute       int
	PolishPerMinute   int
	EventsPerMinute   int
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
			SecureCookies:     boolean("ADMIN_SECURE_COOKIES", str("APP_ENV", "development") == "production"),
		},
		Payments: Payments{
			Mode:         str("PAYMENT_MODE", "demo"),
			DemoCheckout: boolean("PAYMENT_DEMO_CHECKOUT", false),
		},
		Limits: Limits{
			SourceTextChars:   num("LIMIT_SOURCE_TEXT_CHARS", 400),
			InstructionChars:  num("LIMIT_INSTRUCTION_CHARS", 400),
			RequestBodyBytes:  int64(num("LIMIT_REQUEST_BODY_BYTES", 32*1024)),
			OTPRequestPerHour: num("RATE_OTP_REQUEST_PER_HOUR", 5),
			OTPVerifyPerHour:  num("RATE_OTP_VERIFY_PER_HOUR", 10),
			AIPerMinute:       num("RATE_AI_PER_MINUTE", 12),
			PolishPerMinute:   num("RATE_POLISH_PER_MINUTE", 20),
			EventsPerMinute:   num("RATE_EVENTS_PER_MINUTE", 30),
			AdminLoginPerHour: num("RATE_ADMIN_LOGIN_PER_HOUR", 10),
			GenericPerMinute:  num("RATE_GENERIC_PER_MINUTE", 60),
		},
		Log:  Log{Level: str("LOG_LEVEL", "info"), Format: str("LOG_FORMAT", "json")},
		Push: pushFromEnv(),
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
	if c.App.IsProduction() {
		// Демо OTP өндірісте автоматты түрде тыйым салынады.
		if c.Auth.DemoMode {
			problems = append(problems, "AUTH_DEMO_MODE must be false when APP_ENV=production")
		}
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

func contains(all []string, v string) bool {
	for _, a := range all {
		if a == v {
			return true
		}
	}
	return false
}
