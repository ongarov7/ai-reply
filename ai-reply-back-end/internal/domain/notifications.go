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

// CampaignCategories — әкімші науқанына рұқсат етілген санаттар.
//
// Security and account notices reach people who switched everything else off
// and are delivered as important: only the server's own events may use them,
// never a campaign written in the admin panel.
var CampaignCategories = []string{CategorySubscription, CategorySystem, CategoryMarketing}

// IsCampaignCategory — науқанға рұқсат етілген санат па.
func IsCampaignCategory(v string) bool {
	for _, c := range CampaignCategories {
		if c == v {
			return true
		}
	}
	return false
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

// CategoryOptIn — қолданушы өзі қоспайынша өшірулі санат (жарнама).
//
// Marketing needs an explicit yes: an account without a stored choice gets
// none. The other optional categories stay on until switched off.
func CategoryOptIn(category string) bool { return category == CategoryMarketing }

// CategoryEnabled — сақталған таңдау (stored: санат → қосулы ма) бойынша санат қосулы ма.
func CategoryEnabled(category string, stored map[string]bool) bool {
	if !CategoryOptional(category) {
		return true
	}
	enabled, set := stored[category]
	if !set {
		return !CategoryOptIn(category)
	}
	return enabled
}

// CategoryImportant — жоғары басымдықпен және «маңызды» арнамен жеткізілетін санаттар.
func CategoryImportant(category string) bool {
	return category == CategoryAccount || category == CategorySubscription || category == CategorySecurity
}

// Хабарлама түрлері. campaign — әкімші науқаны, қалғандары — сервердің автоматты оқиғалары.
const (
	TypeCampaign              = "campaign"
	TypeSubscriptionActivated = "subscription_activated"
	TypeSubscriptionExpiring  = "subscription_expiring"
	TypeSubscriptionExpired   = "subscription_expired"
	TypeQuotaLow              = "quota_low"
	TypeQuotaExhausted        = "quota_exhausted"
)

// AutomaticNotificationTypes — сервер өзі жіберетін түрлер (әкімші сүзгісі үшін).
var AutomaticNotificationTypes = []string{
	TypeSubscriptionActivated, TypeSubscriptionExpiring, TypeSubscriptionExpired, TypeQuotaLow, TypeQuotaExhausted,
}

// Жеткізу арналары.
const (
	ChannelPush  = "push"
	ChannelEmail = "email"
)

// Push жеткізушісі: Android те, iOS та FCM арқылы (iOS-қа FCM өзі APNs-пен жеткізеді).
const ProviderFCM = "fcm"

// ProviderEmail — пошта жеткізушісі (delivery.provider).
const ProviderEmail = "resend"

// ProviderFor — платформаға жеткізетін провайдер ("" — push жоқ).
func ProviderFor(platform string) string {
	switch platform {
	case PlatformAndroid, PlatformIOS:
		return ProviderFCM
	default:
		return ""
	}
}

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
// тек қосымшаның POST /api/v1/notifications/opened сұранысы растайды (opened_at).
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

// Notification — бір логикалық хабарлама: науқанның бір тілі не бір қолданушыға бір оқиға.
//
// DedupeKey is unique in the database. A second attempt to create the same
// logical notification (a retried request, a repeated event, a second
// backend instance) finds the first row instead of creating another one.
// Params are the e-mail template values; they stay on the server and are
// never part of a push payload.
type Notification struct {
	ID             string
	DedupeKey      string
	IdempotencyKey string
	CampaignID     string
	UserID         string
	Category       string
	Type           string
	Locale         string
	Title          string
	Body           string
	Link           string
	Data           map[string]string
	Params         map[string]string
	CreatedAt      time.Time
}

// Delivery — бір хабарламаның бір арнамен жеткізілуі (outbox жолы).
// Email rows and the single "push was not sent" row have no installation.
type Delivery struct {
	ID                string
	NotificationID    string
	CampaignID        string
	Channel           string
	InstallationID    string
	UserID            string
	Platform          string
	Provider          string
	Status            string
	AttemptCount      int
	NextAttemptAt     time.Time
	ProviderMessageID string
	TokenFingerprint  string // push: the token the last attempt used, fcm:1a2b3c4d
	ErrorCode         string
	ErrorDetail       string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	SentAt            *time.Time
	FailedAt          *time.Time
	OpenedAt          *time.Time
}

// Аудитория сегменттері.
const (
	SegmentAll  = "all"
	SegmentFree = "free"
	SegmentPaid = "paid"
	SegmentDemo = "demo"
)

// Аудиторияның жазылым және квота сүзгілері.
const (
	SubscriptionFilterActive  = "active"
	SubscriptionFilterExpired = "expired"

	QuotaHasRemaining  = "has_remaining"
	QuotaNearExhausted = "near_exhaustion"
	QuotaExhausted     = "exhausted"
)

// QuotaLowThreshold — «квота аяқталуға жақын» шегі: max(1, ceil(limit × percent / 100)).
// A limit of 7 at 10% gives 1, so even the free plan has a "last one left"
// moment before it is exhausted. The audience filter uses the same rule in SQL.
func QuotaLowThreshold(limit, percent int) int {
	return max(1, (limit*percent+99)/100)
}

// AudienceFilter — науқан алушыларын таңдау. Барлық шарт ЖӘНЕ арқылы біріктіріледі.
//
// Only devices attached to an active account (kind "account") are ever
// targeted. UserIDs and Emails together name specific people: an e-mail is
// resolved to its account on the server, and the device must belong to one of
// the named accounts. The backend turns the filter into SQL; the browser never
// decides who receives a campaign.
type AudienceFilter struct {
	Segment      string   `json:"segment,omitempty"`      // free | paid | demo ("" or "all" — everyone)
	PlanIDs      []string `json:"plan_ids,omitempty"`     // the account's current plan
	Subscription string   `json:"subscription,omitempty"` // active | expired (a paid plan now / had one, none now)
	Platforms    []string `json:"platforms,omitempty"`
	Languages    []string `json:"languages,omitempty"` // the language the notification would be sent in
	Quota        string   `json:"quota,omitempty"`     // has_remaining | near_exhaustion | exhausted (today)
	UserIDs      []string `json:"user_ids,omitempty"`
	Emails       []string `json:"emails,omitempty"`
}

// Specific — сүзгі нақты адамдарды атайды ма.
func (f AudienceFilter) Specific() bool { return len(f.UserIDs) > 0 || len(f.Emails) > 0 }

// LocalizedText — бір тілдегі тақырып пен мәтін.
type LocalizedText struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Filled — тақырып та, мәтін де бар.
func (t LocalizedText) Filled() bool { return t.Title != "" && t.Body != "" }

// Campaign — әкімші жіберетін жаппай хабарлама.
//
// Content holds the text per language; Title and Body repeat the fallback
// language's text (the table's original columns).
type Campaign struct {
	ID             string
	Name           string
	Title          string
	Body           string
	Content        map[string]LocalizedText
	FallbackLocale string
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

// TextFor — тілге жіберілетін мәтін: сол тіл толық болса — сол, әйтпесе қор тіл,
// ол да бос болса — kk, ru, en, uz ретімен алғашқы толтырылғаны.
func (c Campaign) TextFor(locale string) (LocalizedText, string) {
	if t := c.Content[locale]; t.Filled() {
		return t, locale
	}
	if t := c.Content[c.FallbackLocale]; t.Filled() {
		return t, c.FallbackLocale
	}
	for _, l := range Locales {
		if t := c.Content[l]; t.Filled() {
			return t, l
		}
	}
	return LocalizedText{Title: c.Title, Body: c.Body}, c.FallbackLocale
}

// DeliveryStats — науқан не сүзгі бойынша жеткізу есебі.
type DeliveryStats struct {
	Total        int                      `json:"total"`
	Queued       int                      `json:"queued"`
	Sending      int                      `json:"sending"`
	Retrying     int                      `json:"retrying"`
	Accepted     int                      `json:"provider_accepted"`
	Failed       int                      `json:"provider_failed"`
	InvalidToken int                      `json:"invalid_token"`
	Skipped      int                      `json:"skipped"`
	Cancelled    int                      `json:"cancelled"`
	Opened       int                      `json:"opened"`
	Android      int                      `json:"android"`
	IOS          int                      `json:"ios"`
	ByLanguage   map[string]LanguageStats `json:"by_language"`
}

// LanguageStats — бір тілге жіберілген хабарламалардың есебі.
type LanguageStats struct {
	Total    int `json:"total"`
	Pending  int `json:"pending"`
	Accepted int `json:"provider_accepted"`
	Failed   int `json:"failed"` // provider_failed + invalid_token
	Skipped  int `json:"skipped"`
	Opened   int `json:"opened"`
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

// AddLanguage — бір тілдің бір күйдегі жолдары.
func (s *DeliveryStats) AddLanguage(locale, status string, n, opened int) {
	if s.ByLanguage == nil {
		s.ByLanguage = map[string]LanguageStats{}
	}
	l := s.ByLanguage[locale]
	l.Total += n
	l.Opened += opened
	switch {
	case DeliveryPending(status):
		l.Pending += n
	case status == DeliveryAccepted:
		l.Accepted += n
	case status == DeliveryFailed || status == DeliveryInvalidToken:
		l.Failed += n
	case status == DeliverySkipped:
		l.Skipped += n
	}
	s.ByLanguage[locale] = l
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
// 1.3.2). An unparsable version is 0.
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

// LinkScreens — deep link экрандары: қосымша оларды өз навигациясына аударады.
var LinkScreens = []string{
	"home", "subscription", "settings", "notifications", "templates", "profile", "keyboard", "compose",
}
