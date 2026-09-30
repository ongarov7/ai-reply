package domain

import (
	"strconv"
	"strings"
	"time"
)

// Push хабарламаларының санаттары. Қолданушы қауіпсіздіктен басқасын өшіре алады.
const (
	CategoryAccount      = "account"
	CategorySubscription = "subscription"
	CategorySecurity     = "security"
	CategorySystem       = "system"
	CategoryMarketing    = "marketing"
)

// NotificationCategories — барлық санат, көрсету ретімен.
var NotificationCategories = []string{
	CategoryAccount, CategorySubscription, CategorySecurity, CategorySystem, CategoryMarketing,
}

// IsNotificationCategory — белгілі санат па.
func IsNotificationCategory(v string) bool {
	for _, c := range NotificationCategories {
		if c == v {
			return true
		}
	}
	return false
}

// CategoryOptional — қолданушы бұл санатты өшіре ала ма (қауіпсіздік хабарлары әрқашан келеді).
func CategoryOptional(category string) bool { return category != CategorySecurity }

// CategoryImportant — жоғары басымдықпен және «маңызды» арнамен жеткізілетін санаттар.
func CategoryImportant(category string) bool {
	return category == CategoryAccount || category == CategorySubscription || category == CategorySecurity
}

// Push жеткізушілері.
const (
	ProviderFCM  = "fcm"
	ProviderAPNs = "apns"
)

// ProviderFor — платформаға жеткізетін провайдер ("" — push жоқ).
func ProviderFor(platform string) string {
	switch platform {
	case PlatformAndroid:
		return ProviderFCM
	case PlatformIOS:
		return ProviderAPNs
	default:
		return ""
	}
}

// APNs орталары.
const (
	APNsSandbox    = "sandbox"
	APNsProduction = "production"
)

// ОЖ хабарлама рұқсатын қалай хабарлайды.
const (
	PermissionAuthorized    = "authorized"
	PermissionDenied        = "denied"
	PermissionNotDetermined = "not_determined"
	PermissionProvisional   = "provisional"
	PermissionEphemeral     = "ephemeral"
	PermissionUnknown       = "unknown"
)

// PermissionAllowsAlerts — бұл рұқсатпен хабарлама экранда көріне ме.
func PermissionAllowsAlerts(p string) bool {
	switch p {
	case PermissionAuthorized, PermissionProvisional, PermissionEphemeral, PermissionUnknown:
		return true
	default:
		return false
	}
}

// Орнатудың push күйі.
const (
	PushNone     = "none"     // токен әлі жоқ
	PushActive   = "active"   // токен жарамды
	PushInvalid  = "invalid"  // провайдер токенді қабылдамады
	PushReplaced = "replaced" // бұл токен басқа орнатуға көшті
)

// Installation — қосымшаның бір құрылғыдағы бір орнатуы.
//
// The client generates InstallationID and keeps it for the life of the
// install; ID is the server's own key. The raw push token never leaves the
// repository layer: this type carries only its hash.
type Installation struct {
	ID                   string
	InstallationID       string
	UserID               string
	Platform             string
	AppVersion           string
	AppBuild             string
	OSName               string
	OSVersion            string
	DeviceModel          string
	Manufacturer         string
	Locale               string
	Timezone             string
	PushProvider         string
	PushEnvironment      string
	PushTokenHash        string
	PushPermission       string
	NotificationsEnabled bool
	PushStatus           string
	PushStatusReason     string
	TokenUpdatedAt       *time.Time
	PushDisabledAt       *time.Time
	AttachedAt           *time.Time
	FirstSeenAt          time.Time
	LastSeenAt           time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// HasToken — жарамды push токені бар ма.
func (i Installation) HasToken() bool { return i.PushTokenHash != "" && i.PushStatus == PushActive }

// TokenFingerprint — журнал мен әкімші панеліне жарамды қысқа белгі (токеннің өзі емес).
func (i Installation) TokenFingerprint() string {
	return TokenFingerprint(i.PushProvider, i.PushTokenHash)
}

// TokenFingerprint — провайдер + хэштің алғашқы 8 таңбасы, мысалы "fcm:1a2b3c4d".
func TokenFingerprint(provider, hash string) string {
	if hash == "" {
		return ""
	}
	if len(hash) > 8 {
		hash = hash[:8]
	}
	if provider == "" {
		return hash
	}
	return provider + ":" + hash
}

// ShortID — ұзын идентификатордың көрсетуге жарамды қысқа түрі.
func ShortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// Жеткізу күйлері. Провайдердің қабылдауы оқылды дегенді білдірмейді: ашылғанын
// тек қосымшаның notification_opened оқиғасы растайды (opened_at).
const (
	DeliveryQueued       = "queued"
	DeliverySending      = "sending"
	DeliveryRetrying     = "retrying"
	DeliveryAccepted     = "provider_accepted"
	DeliveryFailed       = "provider_failed"
	DeliveryInvalidToken = "invalid_token"
	DeliverySkipped      = "skipped"
	DeliveryCancelled    = "cancelled"
)

// DeliveryStatuses — барлық күй (сүзгі тексеру үшін).
var DeliveryStatuses = []string{
	DeliveryQueued, DeliverySending, DeliveryRetrying, DeliveryAccepted, DeliveryFailed,
	DeliveryInvalidToken, DeliverySkipped, DeliveryCancelled,
}

// DeliveryPending — жеткізу әлі аяқталмаған күйлер.
func DeliveryPending(status string) bool {
	return status == DeliveryQueued || status == DeliverySending || status == DeliveryRetrying
}

// Науқан күйлері.
const (
	CampaignDraft           = "draft"
	CampaignQueued          = "queued"
	CampaignProcessing      = "processing"
	CampaignCompleted       = "completed"
	CampaignPartiallyFailed = "partially_failed"
	CampaignFailed          = "failed"
	CampaignCancelled       = "cancelled"
)

// CampaignStatuses — барлық күй.
var CampaignStatuses = []string{
	CampaignDraft, CampaignQueued, CampaignProcessing, CampaignCompleted,
	CampaignPartiallyFailed, CampaignFailed, CampaignCancelled,
}

// Notification — бір логикалық хабарлама: бір науқан не бір қолданушыға бір оқиға.
//
// DedupeKey is unique in the database. A second attempt to create the same
// logical notification (a retried request, a repeated event, a second
// backend instance) finds the first row instead of creating another one.
type Notification struct {
	ID             string
	DedupeKey      string
	IdempotencyKey string
	CampaignID     string
	UserID         string
	Category       string
	Type           string
	Title          string
	Body           string
	Link           string
	Data           map[string]string
	CreatedAt      time.Time
}

// Delivery — бір хабарламаның бір орнатуға жеткізілуі (outbox жолы).
type Delivery struct {
	ID                string
	NotificationID    string
	CampaignID        string
	InstallationID    string
	UserID            string
	Platform          string
	Provider          string
	Status            string
	AttemptCount      int
	NextAttemptAt     time.Time
	ProviderMessageID string
	ErrorCode         string
	ErrorDetail       string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	SentAt            *time.Time
	FailedAt          *time.Time
	OpenedAt          *time.Time
}

// Орнатудың тіркелгіге байланысы (аудитория және құрылғылар тізімінің сүзгісі).
const (
	AuthAuthenticated = "authenticated"
	AuthAnonymous     = "anonymous"
)

// AudienceFilter — науқан алушыларын таңдау. Барлық шарт ЖӘНЕ арқылы біріктіріледі.
//
// The backend turns it into SQL over installations, users and subscriptions;
// the browser never decides who receives a campaign.
type AudienceFilter struct {
	Platforms        []string `json:"platforms,omitempty"`
	Auth             string   `json:"auth,omitempty"`         // authenticated | anonymous
	Payment          string   `json:"payment,omitempty"`      // paid | unpaid
	Subscription     string   `json:"subscription,omitempty"` // active | expired | none
	Locales          []string `json:"locales,omitempty"`
	AppVersionMin    string   `json:"app_version_min,omitempty"`
	AppVersionMax    string   `json:"app_version_max,omitempty"`
	OSVersionMin     string   `json:"os_version_min,omitempty"`
	ActiveWithinDays int      `json:"active_within_days,omitempty"`
	InactiveForDays  int      `json:"inactive_for_days,omitempty"`
	RegisteredFrom   string   `json:"registered_from,omitempty"` // YYYY-MM-DD
	RegisteredTo     string   `json:"registered_to,omitempty"`   // YYYY-MM-DD
	UserIDs          []string `json:"user_ids,omitempty"`
}

// NeedsAccount — сүзгі тек тіркелгісі бар орнатуларға қатысты ма.
func (f AudienceFilter) NeedsAccount() bool {
	return f.Auth == AuthAuthenticated || f.Payment != "" || f.Subscription != "" ||
		f.RegisteredFrom != "" || f.RegisteredTo != "" || len(f.UserIDs) > 0
}

// Campaign — әкімші жіберетін жаппай хабарлама.
type Campaign struct {
	ID             string
	Name           string
	Title          string
	Body           string
	Category       string
	Link           string
	Data           map[string]string
	Audience       AudienceFilter
	Status         string
	CreatedBy      string
	CreatedByEmail string // resolved from admin_users when read
	IdempotencyKey string
	RecipientCount int
	DeviceCount    int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	QueuedAt       *time.Time
	StartedAt      *time.Time
	CompletedAt    *time.Time
	CancelledAt    *time.Time
	FinalStats     *DeliveryStats
}

// DeliveryStats — науқан не сүзгі бойынша жеткізу есебі.
type DeliveryStats struct {
	Total        int `json:"total"`
	Queued       int `json:"queued"`
	Sending      int `json:"sending"`
	Retrying     int `json:"retrying"`
	Accepted     int `json:"provider_accepted"`
	Failed       int `json:"provider_failed"`
	InvalidToken int `json:"invalid_token"`
	Skipped      int `json:"skipped"`
	Cancelled    int `json:"cancelled"`
	Opened       int `json:"opened"`
	Android      int `json:"android"`
	IOS          int `json:"ios"`
}

// Pending — әлі аяқталмаған жеткізулер саны.
func (s DeliveryStats) Pending() int { return s.Queued + s.Sending + s.Retrying }

// Add — бір күйдегі жолдар санын қосады.
func (s *DeliveryStats) Add(status string, n int) {
	s.Total += n
	switch status {
	case DeliveryQueued:
		s.Queued += n
	case DeliverySending:
		s.Sending += n
	case DeliveryRetrying:
		s.Retrying += n
	case DeliveryAccepted:
		s.Accepted += n
	case DeliveryFailed:
		s.Failed += n
	case DeliveryInvalidToken:
		s.InvalidToken += n
	case DeliverySkipped:
		s.Skipped += n
	case DeliveryCancelled:
		s.Cancelled += n
	}
}

// FinalCampaignStatus — барлық жеткізу аяқталғанда науқанның қорытынды күйі.
func FinalCampaignStatus(s DeliveryStats) string {
	failed := s.Failed + s.InvalidToken
	switch {
	case s.Total == 0 || failed == 0:
		return CampaignCompleted
	case s.Accepted == 0:
		return CampaignFailed
	default:
		return CampaignPartiallyFailed
	}
}

// VersionNumber — "1.3.2" → 1003002: нұсқаларды SQL-де салыстыруға болатын сан.
//
// Up to three numeric components, each capped at 999; anything after the
// first non-numeric character of a component is ignored ("1.3.2-beta" is
// 1.3.2). An unparsable version is 0, which no minimum-version filter matches.
func VersionNumber(v string) int64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	parts := strings.SplitN(v, ".", 4)
	var out int64
	for i := 0; i < 3; i++ {
		out *= 1000
		if i >= len(parts) {
			continue
		}
		digits := parts[i]
		for j, r := range digits {
			if r < '0' || r > '9' {
				digits = digits[:j]
				break
			}
		}
		if digits == "" {
			if i == 0 {
				return 0
			}
			continue
		}
		n, err := strconv.ParseInt(digits, 10, 64)
		if err != nil {
			return 0
		}
		if n > 999 {
			n = 999
		}
		out += n
	}
	return out
}

// Deep link экрандары: қосымша оларды өз навигациясына аударады.
var LinkScreens = []string{
	"home", "subscription", "settings", "notifications", "templates", "profile", "keyboard", "compose",
}
