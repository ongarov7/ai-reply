// Package domain — домендік нысандар мен қателер. Мұнда SQL де, HTTP та жоқ.
package domain

import (
	"errors"
	"fmt"
	"time"
)

// Қолданушының күйі.
const (
	UserActive   = "active"
	UserDisabled = "disabled"
)

// Жазылым күйлері.
const (
	SubTrial          = "trial"
	SubActive         = "active"
	SubExpired        = "expired"
	SubCancelled      = "cancelled"
	SubPaymentPending = "payment_pending"
)

// Платформалар.
const (
	PlatformIOS     = "ios"
	PlatformAndroid = "android"
	PlatformWeb     = "web"
	PlatformLegacy  = "legacy"
)

// Қолдау көрсетілетін тілдер.
var Locales = []string{"kk", "ru", "en", "uz"}

// NormalizeLocale белгісіз тілді ағылшынға түсіреді.
func NormalizeLocale(v string) string {
	if len(v) > 2 {
		v = v[:2]
	}
	for _, l := range Locales {
		if l == v {
			return l
		}
	}
	return "en"
}

// User — есептік жазба. Мұнда хабарлама мазмұны ешқашан болмайды.
type User struct {
	ID           string
	Phone        string
	Email        string
	Status       string
	Locale       string
	Timezone     string
	Platform     string
	AppVersion   string
	OSVersion    string
	Kind         string
	LegacyClient string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastActiveAt *time.Time
}

// Identifier — көрсетуге жарамды негізгі идентификатор.
func (u User) Identifier() string {
	if u.Phone != "" {
		return u.Phone
	}
	if u.Email != "" {
		return u.Email
	}
	return u.ID
}

// Profile — жауап дербестендіру үшін қажет ең аз мәлімет.
type Profile struct {
	UserID              string
	DisplayName         string
	Role                string
	Description         string
	PreferredTone       string
	BusinessOffering    string
	BusinessSummary     string
	BusinessRules       []string
	OnboardingCompleted bool
	UpdatedAt           time.Time
}

// LegalConsent records the exact public document versions accepted by an account.
type LegalConsent struct {
	ID             string
	UserID         string
	TermsVersion   string
	PrivacyVersion string
	AcceptedAt     time.Time
	Locale         string
	Platform       string
	AppVersion     string
	CreatedAt      time.Time
}

// Device — тіркелген құрылғы (push негізі осында).
type Device struct {
	ID         string
	UserID     string
	Platform   string
	AppVersion string
	OSVersion  string
	Model      string
	Locale     string
	PushToken  string
	PushOn     bool
	CreatedAt  time.Time
	LastSeenAt time.Time
	RevokedAt  *time.Time
}

// Plan — тариф. Лимиттер кодта емес, дерекқорда.
type Plan struct {
	ID           string
	Code         string
	Name         map[string]string
	Description  map[string]string
	Price        int64
	Currency     string
	DailyLimit   int
	MonthlyLimit int
	PeriodDays   int
	IsFree       bool
	IsActive     bool
	SortOrder    int
	CreatedAt    time.Time
	UpdatedAt    time.Time
	ArchivedAt   *time.Time
}

// Localized таңдалған тілдегі атауды қайтарады (болмаса — ағылшынша).
func (p Plan) LocalizedName(locale string) string { return pick(p.Name, locale) }

// LocalizedDescription — сипаттаманың аудармасы.
func (p Plan) LocalizedDescription(locale string) string { return pick(p.Description, locale) }

func pick(m map[string]string, locale string) string {
	if m == nil {
		return ""
	}
	if v := m[NormalizeLocale(locale)]; v != "" {
		return v
	}
	if v := m["en"]; v != "" {
		return v
	}
	for _, v := range m {
		if v != "" {
			return v
		}
	}
	return ""
}

// Subscription — қолданушының тарифке қатысы.
type Subscription struct {
	ID          string
	UserID      string
	PlanID      string
	Status      string
	Source      string
	StartedAt   time.Time
	ExpiresAt   *time.Time
	CancelledAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// IsUsable — квота беруге жарамды күй ме.
func (s Subscription) IsUsable(now time.Time) bool {
	if s.Status != SubActive && s.Status != SubTrial {
		return false
	}
	if s.ExpiresAt != nil && now.After(*s.ExpiresAt) {
		return false
	}
	return true
}

// Entitlement — қолданушының нақты дәл қазіргі құқығы.
type Entitlement struct {
	Plan         Plan
	Subscription *Subscription
	DailyLimit   int
	MonthlyLimit int
	UsedToday    int
	UsedMonth    int
	ResetsAt     time.Time
}

// Remaining — бүгін қалған генерация саны.
func (e Entitlement) Remaining() int {
	if e.DailyLimit <= 0 {
		return 0
	}
	if e.UsedToday >= e.DailyLimit {
		return 0
	}
	return e.DailyLimit - e.UsedToday
}

// UsageEvent — тек метадерек. source_text те, жауап та жоқ.
type UsageEvent struct {
	ID           string
	UserID       string
	DeviceID     string
	PlanID       string
	Model        string
	Status       string
	ErrorCode    string
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	CostMicros   int64
	LatencyMS    int
	ProviderMS   int
	Platform     string
	AppVersion   string
	Language     string
	SourceChars  int
	CreatedAt    time.Time
}

// AdminUser — әкімші тіркелгісі (мобильді қолданушыдан бөлек).
type AdminUser struct {
	ID           string
	Email        string
	Name         string
	PasswordHash string
	Role         string
	Locale       string
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastLoginAt  *time.Time
}

// AuditEntry — әкімшінің әрбір маңызды әрекеті.
type AuditEntry struct {
	ID         string
	AdminID    string
	AdminEmail string
	Action     string
	EntityType string
	EntityID   string
	Metadata   map[string]any
	IP         string
	CreatedAt  time.Time
}

// Payment — төлем жазбасы (demo адаптерінде де нақты жазба қалады).
type Payment struct {
	ID          string
	UserID      string
	PlanID      string
	Provider    string
	ProviderRef string
	Amount      int64
	Currency    string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Домендік қателер — HTTP қабаты бұларды тұрақты кодтарға айналдырады.
var (
	ErrNotFound         = errors.New("not found")
	ErrConflict         = errors.New("conflict")
	ErrInvalidOTP       = errors.New("invalid otp")
	ErrOTPExpired       = errors.New("otp expired")
	ErrUnauthorized     = errors.New("unauthorized")
	ErrAccountDisabled  = errors.New("account disabled")
	ErrDailyLimit       = errors.New("daily limit reached")
	ErrMonthlyLimit     = errors.New("monthly limit reached")
	ErrSubscriptionGone = errors.New("subscription expired")
	ErrInvalidRequest   = errors.New("invalid request")
	ErrRateLimited      = errors.New("rate limited")
	ErrProviderDown     = errors.New("ai provider unavailable")
	ErrProviderTimeout  = errors.New("ai provider timeout")
	ErrEmptyCompletion  = errors.New("empty completion")
	ErrPaymentRequired  = errors.New("payment required")
	ErrDemoDisabled     = errors.New("demo authentication disabled")

	// ErrSourceTooLong — көшірілген хабарлама әкімші бекіткен шектен ұзын.
	// ErrInvalidRequest-ті орайды: ескі клиенттер бұрынғыдай INVALID_REQUEST
	// алады, жаңалары details ішінен нақты шекті оқиды.
	ErrSourceTooLong = fmt.Errorf("%w: source text too long", ErrInvalidRequest)
)
