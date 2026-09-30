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

// Кіру тәсілдері (auth_identities.kind).
const (
	IdentityPhone  = "phone"
	IdentityEmail  = "email"
	IdentityGoogle = "google"
	IdentityApple  = "apple"
)

// OTP мақсаттары (otp_codes.purpose).
const (
	OTPPurposeLogin     = "login"
	OTPPurposeLinkEmail = "link_email"
)

// OTP жабылу себептері (otp_codes.consumed_reason).
const (
	OTPReasonVerified       = "verified"
	OTPReasonSuperseded     = "superseded"
	OTPReasonExpired        = "expired"
	OTPReasonAttempts       = "attempts"
	OTPReasonDeliveryFailed = "delivery_failed"
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

// Identity — қолданушының бір кіру тәсілі.
//
// Value is the verified e-mail or phone for those kinds, and the provider's
// immutable subject (`sub`) for Google and Apple — never their e-mail, which
// can change or be a private relay.
type Identity struct {
	ID                    string
	UserID                string
	Kind                  string
	Value                 string
	Country               string
	ProviderEmail         string
	ProviderEmailVerified bool
	VerifiedAt            *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
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
	RequestID  string
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

	// Кіру: пошта, OTP, Google және Apple.
	ErrInvalidEmail            = errors.New("invalid email")
	ErrOTPAlreadyUsed          = errors.New("otp already used")
	ErrOTPAttemptsExceeded     = errors.New("otp attempts exceeded")
	ErrOTPCooldown             = errors.New("otp resend cooldown")
	ErrEmailDelivery           = errors.New("email delivery failed")
	ErrEmailInUse              = errors.New("email belongs to another account")
	ErrInvalidIDToken          = errors.New("invalid identity token")
	ErrAuthProviderUnavailable = errors.New("auth provider unavailable")

	// ErrSourceTooLong — көшірілген хабарлама әкімші бекіткен шектен ұзын.
	// ErrInvalidRequest-ті орайды: ескі клиенттер бұрынғыдай INVALID_REQUEST
	// алады, жаңалары details ішінен нақты шекті оқиды.
	ErrSourceTooLong = fmt.Errorf("%w: source text too long", ErrInvalidRequest)
)

// RetryAfterError — қатемен бірге қайта сұрауға болатын уақыт.
//
// The HTTP layer turns it into a Retry-After header and a
// `retry_after_seconds` detail, so a client can count down without guessing.
type RetryAfterError struct {
	Err   error
	After time.Duration
}

// RetryAfter — err-ді күту уақытымен орайды.
func RetryAfter(err error, after time.Duration) error {
	return &RetryAfterError{Err: err, After: after}
}

func (e *RetryAfterError) Error() string { return e.Err.Error() }
func (e *RetryAfterError) Unwrap() error { return e.Err }

// Seconds — жоғары қарай дөңгелектелген, кемінде 1 секунд.
func (e *RetryAfterError) Seconds() int {
	seconds := int((e.After + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

// Details — клиентке кететін қосымша мәлімет.
func (e *RetryAfterError) Details() map[string]any {
	return map[string]any{"retry_after_seconds": e.Seconds()}
}

// OTPAttemptError — код қате; қанша әрекет қалғаны айтылады.
type OTPAttemptError struct{ Remaining int }

func (e *OTPAttemptError) Error() string { return ErrInvalidOTP.Error() }
func (e *OTPAttemptError) Unwrap() error { return ErrInvalidOTP }

// Details — клиентке кететін қосымша мәлімет.
func (e *OTPAttemptError) Details() map[string]any {
	return map[string]any{"attempts_remaining": e.Remaining}
}

// Push хабарламалары.
var (
	// ErrPushDisabled — PUSH_NOTIFICATIONS_ENABLED=false не бірде-бір провайдер бапталмаған.
	ErrPushDisabled = errors.New("push notifications are not configured")
)

// FieldError — сұраныстың қай өрісі жарамсыз (400 INVALID_REQUEST + details.field).
type FieldError struct {
	Field  string
	Reason string
}

func (e *FieldError) Error() string {
	if e.Reason != "" {
		return "invalid " + e.Field + ": " + e.Reason
	}
	return "invalid " + e.Field
}

// Unwrap — ErrInvalidRequest: ескі клиенттер бұрынғыдай INVALID_REQUEST алады.
func (e *FieldError) Unwrap() error { return ErrInvalidRequest }

// Details — клиентке қай өріс екені (мәннің өзі емес).
func (e *FieldError) Details() map[string]any {
	d := map[string]any{"field": e.Field}
	if e.Reason != "" {
		d["reason"] = e.Reason
	}
	return d
}

// InvalidField — FieldError жасайды.
func InvalidField(field, reason string) error { return &FieldError{Field: field, Reason: reason} }

// ConflictError — сұраныс бұрынғы күйге қайшы (409 CONFLICT + details.field).
type ConflictError struct {
	Field  string
	Reason string
}

func (e *ConflictError) Error() string { return "conflict on " + e.Field + ": " + e.Reason }

// Unwrap — ErrConflict.
func (e *ConflictError) Unwrap() error { return ErrConflict }

// Details — қай өріс екені.
func (e *ConflictError) Details() map[string]any {
	return map[string]any{"field": e.Field, "reason": e.Reason}
}

// ConflictField — ConflictError жасайды.
func ConflictField(field, reason string) error { return &ConflictError{Field: field, Reason: reason} }
