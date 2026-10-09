// Package notifications — хабарламалар қызметі: жасау, кезек, жеткізу (push және пошта), науқандар.
//
// Business code calls NotifyUser; the admin panel creates campaigns. Both
// write a logical notification and its deliveries (the outbox) in one
// database transaction, keyed so that the same event or the same admin
// command can never produce a second notification. The dispatcher (Run) then
// claims deliveries with a lease, sends push through FCM (Android and iOS)
// and e-mail through the mailer, retries transient failures with backoff and
// switches off tokens the provider reports dead.
//
// Without push configured nothing is lost or refused: an automatic event is
// still recorded, with a skipped push delivery that says why.
package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/email"
	"github.com/aireply/ai-reply-back-end/internal/installations"
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/push"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// StaleAfter — осыдан ұзақ көрінбеген орнатуға жіберілмейді (FCM 270 күннен кейін токенді өшіреді).
const StaleAfter = 270 * 24 * time.Hour

// DefaultTTL — провайдер жеткізілмеген хабарламаны қанша сақтайды.
const DefaultTTL = 24 * time.Hour

// Push жіберілмегенінің себептері (skipped жолдың error_code-ы).
const (
	SkipPushDisabled      = "push_disabled"
	SkipNoDevices         = "no_devices"
	SkipEmailDisabled     = "email_disabled"
	SkipDisabledByUser    = "disabled_by_user"
	SkipRecipientExcluded = "recipient_excluded"
)

// EmailRenderer — хабарлама түрінің хаты алушының тілінде (email.NotificationTemplates).
type EmailRenderer interface {
	Render(notificationType, locale string, params map[string]string) (email.Content, error)
}

// Mailer — бір транзакциялық хатты жібереді (*email.Resend).
type Mailer interface {
	Send(ctx context.Context, msg email.Outgoing) error
}

// Translator — аударма (localization.Bundle.T): шаблон кілттерімен NotifyUser үшін.
type Translator func(locale, key string) string

// Deps — тәуелділіктер.
type Deps struct {
	Repo          *repository.Store
	Installations *installations.Service
	// Provider — FCM: Android and iOS both. Nil when Firebase is not configured.
	Provider push.Provider
	// Plans — the default plan of accounts without a usable subscription (audience filters).
	Plans     *plans.Service
	Config    config.Push
	Location  *time.Location
	Translate Translator
	Log       *slog.Logger
	Clock     traits.Clock
}

// Service — хабарламалар.
type Service struct {
	repo      *repository.Store
	sealer    *installations.Sealer
	provider  push.Provider
	plans     *plans.Service
	cfg       config.Push
	loc       *time.Location
	translate Translator
	log       *slog.Logger
	clock     traits.Clock
	owner     string
	wakeCh    chan struct{}
	jitter    func() float64

	emailMu  sync.RWMutex
	mailer   Mailer
	renderer EmailRenderer
}

// New — қызмет.
func New(d Deps) *Service {
	clock := d.Clock
	if clock == nil {
		clock = traits.SystemClock{}
	}
	loc := d.Location
	if loc == nil {
		loc = time.UTC
	}
	cfg := d.Config
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 8
	}
	if cfg.QuotaLowPercent <= 0 {
		cfg.QuotaLowPercent = 10
	}
	host, _ := os.Hostname()
	var sealer *installations.Sealer
	if d.Installations != nil {
		sealer = d.Installations.Sealer()
	}
	return &Service{
		repo: d.Repo, sealer: sealer, provider: d.Provider, plans: d.Plans, cfg: cfg, loc: loc,
		translate: d.Translate, log: d.Log, clock: clock, wakeCh: make(chan struct{}, 1), jitter: randomJitter,
		owner: fmt.Sprintf("%s:%d:%s", traits.Clamp(host, 32), os.Getpid(), traits.RandomToken(3)),
	}
}

// WithEmail — хабарлама хаттарының жеткізушісі мен үлгілері (nil — хат жіберілмейді).
func (s *Service) WithEmail(mailer Mailer, renderer EmailRenderer) *Service {
	s.emailMu.Lock()
	defer s.emailMu.Unlock()
	s.mailer, s.renderer = mailer, renderer
	return s
}

func (s *Service) email() (Mailer, EmailRenderer) {
	s.emailMu.RLock()
	defer s.emailMu.RUnlock()
	if !s.cfg.EmailsEnabled || s.mailer == nil || s.renderer == nil {
		return nil, nil
	}
	return s.mailer, s.renderer
}

// Status — әкімші панеліне: не бапталған.
type Status struct {
	Enabled   bool     `json:"enabled"` // PUSH_NOTIFICATIONS_ENABLED
	Worker    bool     `json:"worker"`  // the dispatcher runs in this process
	FCM       bool     `json:"fcm"`     // Firebase credentials work (Android and iOS)
	Email     bool     `json:"email"`   // notification e-mails go out (Resend)
	LinkHosts []string `json:"link_hosts"`
}

// Status — ағымдағы күй.
func (s *Service) Status() Status {
	mailer, _ := s.email()
	hosts := s.cfg.LinkHosts
	if hosts == nil {
		hosts = []string{}
	}
	return Status{
		Enabled:   s.cfg.Enabled,
		Worker:    s.cfg.WorkerEnabled && (s.Ready() || mailer != nil),
		FCM:       s.provider != nil,
		Email:     mailer != nil,
		LinkHosts: hosts,
	}
}

// Ready — push жіберуге бола ма (қосулы және FCM бапталған).
func (s *Service) Ready() bool { return s.cfg.Enabled && s.provider != nil }

func (s *Service) platforms() []string {
	if !s.Ready() {
		return nil
	}
	return []string{domain.PlatformAndroid, domain.PlatformIOS}
}

// audienceQuery — сүзгі мен санаттан SQL шарттары: поштаны тіркелгіге айналдырады,
// қажет болса әдепкі тарифті табады. Табылмаған поштаны қайтарады.
func (s *Service) audienceQuery(ctx context.Context, f domain.AudienceFilter, category string) (repository.AudienceQuery, []string, error) {
	now := s.clock.Now()
	q := repository.AudienceQuery{
		Filter: f, Category: category, Now: now, Today: now.In(s.loc).Format("2006-01-02"),
		QuotaLowPercent: s.cfg.QuotaLowPercent, Platforms: s.platforms(), StaleAfter: StaleAfter,
	}
	if (len(f.PlanIDs) > 0 || f.Quota != "") && s.plans != nil {
		plan, err := s.plans.Default(ctx)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return q, nil, err
		}
		q.DefaultPlanID = plan.ID
	}
	var unresolved []string
	if f.Specific() {
		ids, missing, err := s.repo.ResolveAccountEmails(ctx, f.Emails)
		if err != nil {
			return q, nil, err
		}
		q.Recipients = append(append([]string{}, f.UserIDs...), ids...)
		unresolved = missing
	}
	return q, unresolved, nil
}

// wake — жұмысшыны бірден оятады (осы процестегі жаңа хабарлама үшін).
func (s *Service) wake() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

// ---------------------------------------------------------------- user notifications

// UserNotification — бір қолданушыға арналған бір оқиға.
//
// The text is produced in the recipient's language, resolved on the server:
// preferred_language, else the locale of the account's most recently seen
// device, else the account locale, else English. Give either Text or the
// TitleKey/BodyKey pair; keys are translated with the service's Translator
// and their "{name}" placeholders take Params.
type UserNotification struct {
	UserID string
	// IdempotencyKey — the event's own identity, e.g. "subscription_activated:payment:<id>".
	// The same key for the same user never produces a second notification.
	IdempotencyKey string
	Type           string // domain.Type*
	Category       string // domain.Category*
	Link           string // aireply://<screen> or an allowed https URL
	Data           map[string]string
	// Text — the push title and body for the resolved language.
	Text func(locale string) (title, body string)
	// TitleKey, BodyKey — translation keys used when Text is nil.
	TitleKey, BodyKey string
	// Params — template values ({plan}, {date}) for the keys and the e-mail.
	// Stored with the notification on the server; never sent to a device.
	Params map[string]string
	// ParamsFor — values that depend on the resolved language (a plan's
	// localized name); merged over Params once the language is known.
	ParamsFor func(locale string) map[string]string
	// Email — also send one e-mail, rendered at send time by the EmailRenderer
	// from Type, the resolved language and Params, to the account's verified address.
	Email bool
	// Transactional — the e-mail confirms something that happened to the
	// account (a plan bought or assigned): the category switch silences only
	// the push, never this e-mail.
	Transactional bool
}

// NotifyResult — NotifyUser нәтижесі.
type NotifyResult struct {
	NotificationID string
	Created        bool   // false: this event was already turned into a notification
	Locale         string // the language the texts were produced in
	Devices        int    // push deliveries queued now
	Skipped        string // why no push was queued: push_disabled | disabled_by_user | no_devices | recipient_excluded
	Email          bool   // an e-mail delivery was queued now
}

var (
	typePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`)
	keyPattern  = regexp.MustCompile(`^[A-Za-z0-9:_.@-]{1,160}$`)
)

// NotifyUser — бизнес-оқиға → бір логикалық хабарлама → қолданушының құрылғыларына және поштасына.
//
// Safe to call more than once for the same event (a retried request, a
// repeated sweep, two backend processes): the second call finds the first
// notification and queues nothing. With push switched off, or with no device
// to reach, the notification is still recorded with one skipped push
// delivery; that is not an error. Accounts that are disabled, the simulator
// and legacy install tokens are never notified (Skipped "recipient_excluded",
// nothing written).
func (s *Service) NotifyUser(ctx context.Context, n UserNotification) (NotifyResult, error) {
	if strings.TrimSpace(n.UserID) == "" {
		return NotifyResult{}, domain.InvalidField("user_id", "required")
	}
	if !keyPattern.MatchString(n.IdempotencyKey) {
		return NotifyResult{}, domain.InvalidField("idempotency_key", "format")
	}
	if !typePattern.MatchString(n.Type) {
		return NotifyResult{}, domain.InvalidField("type", "format")
	}
	user, err := s.repo.UserByID(ctx, n.UserID)
	if err != nil {
		return NotifyResult{}, err
	}
	if user.Kind != "account" || user.Status != domain.UserActive {
		return NotifyResult{Skipped: SkipRecipientExcluded}, nil
	}
	deviceLocale, err := s.repo.LatestInstallationLocale(ctx, user.ID)
	if err != nil {
		return NotifyResult{}, err
	}
	locale := domain.ResolveLanguage(user.PreferredLanguage, deviceLocale, user.Locale)

	params := n.Params
	if n.ParamsFor != nil {
		params = make(map[string]string, len(n.Params))
		for k, v := range n.Params {
			params[k] = v
		}
		for k, v := range n.ParamsFor(locale) {
			params[k] = v
		}
	}
	params, err = validateParams(params)
	if err != nil {
		return NotifyResult{}, err
	}
	title, body, err := s.render(n, locale, params)
	if err != nil {
		return NotifyResult{}, err
	}
	content, err := s.validateContent(Content{Title: title, Body: body, Category: n.Category, Link: n.Link, Data: n.Data})
	if err != nil {
		return NotifyResult{}, err
	}

	// The person's category switch applies to the push and to every e-mail
	// that is not transactional; security is always on.
	silenced := false
	if domain.CategoryOptional(content.Category) {
		prefs, err := s.repo.NotificationPreferences(ctx, user.ID)
		if err != nil {
			return NotifyResult{}, err
		}
		silenced = !domain.CategoryEnabled(content.Category, prefs)
	}
	channels := repository.UserChannels{Email: n.Email}
	switch {
	case !s.Ready():
		channels.PushSkip = SkipPushDisabled
	case silenced:
		channels.PushSkip = SkipDisabledByUser
	}
	if n.Email {
		if mailer, _ := s.email(); mailer == nil {
			channels.EmailSkip = SkipEmailDisabled
		} else if silenced && !n.Transactional {
			channels.EmailSkip = SkipDisabledByUser
		}
	}

	q, _, err := s.audienceQuery(ctx, domain.AudienceFilter{}, content.Category)
	if err != nil {
		return NotifyResult{}, err
	}
	q.UserID = user.ID
	res, err := s.repo.CreateUserNotification(ctx, domain.Notification{
		DedupeKey:      repository.UserNotificationKey(user.ID, n.IdempotencyKey),
		IdempotencyKey: n.IdempotencyKey,
		UserID:         user.ID,
		Category:       content.Category,
		Type:           n.Type,
		Locale:         locale,
		Title:          content.Title,
		Body:           content.Body,
		Link:           content.Link,
		Data:           content.Data,
		Params:         params,
	}, q, channels)
	if err != nil {
		return NotifyResult{}, err
	}
	s.log.Info("notification created", "event", "notification_created",
		"notification_id", res.ID, "user_id", user.ID, "type", n.Type, "category", content.Category,
		"locale", locale, "created", res.Created, "devices", res.Devices, "skipped", res.Skipped, "email_queued", res.Email)
	if res.Created && (res.Devices > 0 || res.Email) {
		s.wake()
	}
	return NotifyResult{
		NotificationID: res.ID, Created: res.Created, Locale: locale, Devices: res.Devices,
		Skipped: res.Skipped, Email: res.Email,
	}, nil
}

// render — push мәтіні алушының тілінде.
func (s *Service) render(n UserNotification, locale string, params map[string]string) (string, string, error) {
	if n.Text != nil {
		title, body := n.Text(locale)
		return title, body, nil
	}
	if n.TitleKey == "" || n.BodyKey == "" {
		return "", "", domain.InvalidField("title", "give the text or its translation keys")
	}
	if s.translate == nil {
		return "", "", domain.InvalidField("title_key", "no translations are loaded")
	}
	text := func(key string) (string, error) {
		v := s.translate(locale, key)
		if v == "" || v == key {
			return "", domain.InvalidField("title_key", "missing translation "+key)
		}
		for name, value := range params {
			v = strings.ReplaceAll(v, "{"+name+"}", value)
		}
		return v, nil
	}
	title, err := text(n.TitleKey)
	if err != nil {
		return "", "", err
	}
	body, err := text(n.BodyKey)
	return title, body, err
}

// MarkOpened — қосымша хабарламаны ашқанын хабарлады (тек сол орнатуға жіберілген болса жазылады).
func (s *Service) MarkOpened(ctx context.Context, installationID, deliveryID string) (bool, error) {
	if !installations.ValidInstallationID(installationID) {
		return false, domain.InvalidField("installation_id", "format")
	}
	if !deliveryIDPattern.MatchString(deliveryID) {
		return false, domain.InvalidField("delivery_id", "format")
	}
	return s.repo.MarkDeliveryOpened(ctx, deliveryID, installationID, s.clock.Now())
}

var deliveryIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

// ---------------------------------------------------------------- preferences

// Preferences — қолданушының санаттар бойынша баптауы (әр санат үшін нақты мән).
func (s *Service) Preferences(ctx context.Context, userID string) (map[string]bool, error) {
	stored, err := s.repo.NotificationPreferences(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, c := range domain.NotificationCategories {
		// No stored row: on, except marketing, which needs an explicit yes.
		out[c] = domain.CategoryEnabled(c, stored)
	}
	return out, nil
}

// SetPreferences — тек өшіруге болатын санаттар өзгереді.
func (s *Service) SetPreferences(ctx context.Context, userID string, prefs map[string]bool) (map[string]bool, error) {
	clean := map[string]bool{}
	for category, enabled := range prefs {
		if !domain.IsNotificationCategory(category) {
			return nil, domain.InvalidField("preferences."+traits.Clamp(category, 32), "unknown category")
		}
		if !domain.CategoryOptional(category) {
			if !enabled {
				return nil, domain.InvalidField("preferences."+category, "always on")
			}
			continue
		}
		clean[category] = enabled
	}
	if err := s.repo.SetNotificationPreferences(ctx, userID, clean, s.clock.Now()); err != nil {
		return nil, err
	}
	return s.Preferences(ctx, userID)
}

// OptionalCategories — қолданушы өшіре алатын санаттар.
func OptionalCategories() []string {
	out := []string{}
	for _, c := range domain.NotificationCategories {
		if domain.CategoryOptional(c) {
			out = append(out, c)
		}
	}
	return out
}

// ---------------------------------------------------------------- campaigns

// CampaignInput — әкімші формасы. Title мен Body — тіл → мәтін.
type CampaignInput struct {
	Name           string
	Category       string
	FallbackLocale string
	Title          map[string]string
	Body           map[string]string
	Link           string
	Data           map[string]string
	Audience       domain.AudienceFilter
}

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_:.-]{8,128}$`)

// CampaignResult — науқан және осы шақыру нақты не істегені (аудит үшін).
type CampaignResult struct {
	Campaign domain.Campaign
	Created  bool // this call inserted the campaign
	Queued   bool // this call moved it from draft to queued
}

// CreateCampaign — науқан жасайды (send=true болса, бірден кезекке қояды).
//
// The Idempotency-Key makes the command safe to repeat: a double click, a
// retried request after a timeout, or a reload that re-sends the form all
// return the campaign the first request created (Created=false). The same
// key with different content is a conflict. When the send step fails after
// the insert, the result still reports Created=true together with the error,
// so the caller can record what did happen.
func (s *Service) CreateCampaign(ctx context.Context, adminID string, in CampaignInput, idempotencyKey string, send bool) (CampaignResult, error) {
	if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		return CampaignResult{}, domain.InvalidField("idempotency_key", "send an Idempotency-Key header")
	}
	content, fallback, envelope, err := s.validateCampaign(in)
	if err != nil {
		return CampaignResult{}, err
	}
	audience, err := s.validateAudience(ctx, in.Audience)
	if err != nil {
		return CampaignResult{}, err
	}
	name := traits.Clamp(traits.CollapseSpaces(in.Name), 120)
	if name == "" {
		name = traits.Clamp(content[fallback].Title, 120)
	}
	if send && !s.Ready() {
		return CampaignResult{}, domain.ErrPushDisabled
	}
	requested := domain.Campaign{
		Name: name, Title: content[fallback].Title, Body: content[fallback].Body, Content: content,
		FallbackLocale: fallback, Category: envelope.Category, Link: envelope.Link, Data: envelope.Data,
		Audience: audience, Status: domain.CampaignDraft, CreatedBy: adminID, IdempotencyKey: idempotencyKey,
		CreatedAt: s.clock.Now(),
	}
	campaign, created, err := s.repo.CreateCampaign(ctx, requested)
	if err != nil {
		return CampaignResult{}, err
	}
	// A repeated key is a retry of the same form. A key reused for different
	// content is a client bug: answering with the first campaign would hide it.
	if !created && campaignFingerprint(campaign) != campaignFingerprint(requested) {
		return CampaignResult{}, domain.ConflictField("idempotency_key", "already used for a different campaign")
	}
	result := CampaignResult{Campaign: campaign, Created: created}
	if !send {
		return result, nil
	}
	sent, err := s.SendCampaign(ctx, campaign.ID)
	if err != nil {
		return result, err
	}
	sent.Created = created
	return sent, nil
}

// CampaignByIdempotencyKey — осы Idempotency-Key-мен бұрын жасалған науқан (жоқ болса ErrNotFound).
func (s *Service) CampaignByIdempotencyKey(ctx context.Context, key string) (domain.Campaign, error) {
	return s.repo.CampaignByIdempotencyKey(ctx, key)
}

// campaignFingerprint — what a retried create must repeat exactly (content and audience).
func campaignFingerprint(c domain.Campaign) string {
	var b strings.Builder
	for _, part := range []string{c.Name, c.Category, c.Link, c.FallbackLocale} {
		b.WriteString(strconv.Quote(part))
		b.WriteByte('|')
	}
	for _, l := range domain.Locales {
		t := c.Content[l]
		b.WriteString(l + "=" + strconv.Quote(t.Title) + "/" + strconv.Quote(t.Body) + ";")
	}
	keys := make([]string, 0, len(c.Data))
	for k := range c.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(strconv.Quote(k) + "=" + strconv.Quote(c.Data[k]) + ";")
	}
	audience, _ := json.Marshal(c.Audience) // every field is omitempty: nil and empty agree
	b.WriteByte('|')
	b.Write(audience)
	return b.String()
}

// SendCampaign — draft → queued. Қайта шақыру зиянсыз: кезектегі не жіберілген науқан өзгермейді.
//
// Queued reports whether this call made the change; a repeated or concurrent
// send of the same campaign answers with Queued=false.
func (s *Service) SendCampaign(ctx context.Context, id string) (CampaignResult, error) {
	campaign, err := s.repo.Campaign(ctx, id)
	if err != nil {
		return CampaignResult{}, err
	}
	switch campaign.Status {
	case domain.CampaignDraft:
		if !s.Ready() {
			return CampaignResult{}, domain.ErrPushDisabled
		}
		queued, err := s.repo.QueueCampaign(ctx, id, s.clock.Now())
		if err != nil {
			return CampaignResult{}, err
		}
		if queued {
			s.log.Info("campaign queued", "event", "push_campaign_queued", "campaign_id", id)
			s.wake()
		}
		current, err := s.repo.Campaign(ctx, id)
		if err != nil {
			return CampaignResult{}, err
		}
		if !queued && current.Status == domain.CampaignCancelled {
			return CampaignResult{}, domain.ErrConflict // cancelled in the meantime
		}
		return CampaignResult{Campaign: current, Queued: queued}, nil
	case domain.CampaignCancelled:
		return CampaignResult{}, domain.ErrConflict
	default:
		return CampaignResult{Campaign: campaign}, nil
	}
}

// CancelCampaign — әлі жіберілмегенін тоқтатады. Аяқталған науқанды тоқтату — қақтығыс.
// The bool reports whether this call cancelled it (false when it already was).
func (s *Service) CancelCampaign(ctx context.Context, id string) (domain.Campaign, bool, error) {
	campaign, err := s.repo.Campaign(ctx, id)
	if err != nil {
		return domain.Campaign{}, false, err
	}
	switch campaign.Status {
	case domain.CampaignCancelled:
		return campaign, false, nil
	case domain.CampaignDraft, domain.CampaignQueued, domain.CampaignProcessing:
		cancelled, err := s.repo.CancelCampaign(ctx, id, s.clock.Now())
		if err != nil {
			return domain.Campaign{}, false, err
		}
		current, err := s.repo.Campaign(ctx, id)
		if err != nil {
			return domain.Campaign{}, false, err
		}
		if !cancelled && current.Status != domain.CampaignCancelled {
			return domain.Campaign{}, false, domain.ErrConflict // finished in the meantime
		}
		if cancelled {
			s.log.Info("campaign cancelled", "event", "push_campaign_cancelled", "campaign_id", id)
		}
		return current, cancelled, nil
	default:
		return domain.Campaign{}, false, domain.ErrConflict
	}
}

// CampaignView — науқан және оның жеткізу есебі.
type CampaignView struct {
	Campaign domain.Campaign
	Stats    domain.DeliveryStats
	Errors   []repository.ErrorCount
}

// Campaign — науқан, live есеп және қателер бөлінісі.
func (s *Service) Campaign(ctx context.Context, id string) (CampaignView, error) {
	campaign, err := s.repo.Campaign(ctx, id)
	if err != nil {
		return CampaignView{}, err
	}
	view, err := s.view(ctx, campaign)
	if err != nil {
		return CampaignView{}, err
	}
	view.Errors, err = s.repo.CampaignErrors(ctx, id)
	return view, err
}

func (s *Service) view(ctx context.Context, c domain.Campaign) (CampaignView, error) {
	stats, err := s.repo.CampaignStats(ctx, c.ID)
	if err != nil {
		return CampaignView{}, err
	}
	// Аяқталған науқанның есебі — сақталған қорытынды; тірі деректен тек ашылулар.
	//
	// Delivery rows can leave before the campaign does: notification retention,
	// an account deletion (ON DELETE CASCADE) or the anonymous-installation
	// sweep. The counters saved when the campaign finished therefore stay the
	// report; only opens that arrive later are taken from the live rows, and
	// they never drop below the snapshot either. Running campaigns have no
	// snapshot and stay fully live.
	if c.FinalStats != nil {
		live := stats
		stats = *c.FinalStats
		stats.ByLanguage = maps.Clone(c.FinalStats.ByLanguage)
		stats.Opened = max(stats.Opened, live.Opened)
		for lang, ls := range stats.ByLanguage {
			ls.Opened = max(ls.Opened, live.ByLanguage[lang].Opened)
			stats.ByLanguage[lang] = ls
		}
	}
	return CampaignView{Campaign: c, Stats: stats}, nil
}

// Campaigns — тізім (әр науқанның есебімен).
func (s *Service) Campaigns(ctx context.Context, status string, page traits.Page) ([]CampaignView, int, error) {
	if status != "" && !oneOf(status, domain.CampaignStatuses) {
		return nil, 0, domain.InvalidField("status", "unknown")
	}
	list, total, err := s.repo.ListCampaigns(ctx, status, page)
	if err != nil {
		return nil, 0, err
	}
	out := make([]CampaignView, 0, len(list))
	for _, c := range list {
		v, err := s.view(ctx, c)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, nil
}

// Preview — сүзгіге сай алушылар саны, тіл бойынша да. Ешнәрсе жіберілмейді.
func (s *Service) Preview(ctx context.Context, filter domain.AudienceFilter, category string) (repository.AudiencePreview, domain.AudienceFilter, error) {
	audience, err := s.validateAudience(ctx, filter)
	if err != nil {
		return repository.AudiencePreview{}, audience, err
	}
	if category == "" {
		category = domain.CategoryMarketing
	}
	if !domain.IsNotificationCategory(category) {
		return repository.AudiencePreview{}, audience, domain.InvalidField("category", "unknown")
	}
	if !domain.IsCampaignCategory(category) {
		return repository.AudiencePreview{}, audience, domain.InvalidField("category", "not allowed for campaigns")
	}
	q, unresolved, err := s.audienceQuery(ctx, audience, category)
	if err != nil {
		return repository.AudiencePreview{}, audience, err
	}
	preview, err := s.repo.PreviewAudience(ctx, q)
	if unresolved != nil {
		preview.Unresolved = unresolved
	}
	return preview, audience, err
}

// Deliveries — жеткізулер тізімі (әкімші).
func (s *Service) Deliveries(ctx context.Context, f repository.DeliveryFilter) ([]repository.DeliveryRow, int, error) {
	switch {
	case f.Status != "" && !oneOf(f.Status, domain.DeliveryStatuses):
		return nil, 0, domain.InvalidField("status", "unknown")
	case f.Platform != "" && f.Platform != domain.PlatformAndroid && f.Platform != domain.PlatformIOS:
		return nil, 0, domain.InvalidField("platform", "unknown")
	case f.Channel != "" && f.Channel != domain.ChannelPush && f.Channel != domain.ChannelEmail:
		return nil, 0, domain.InvalidField("channel", "unknown")
	case f.Source != "" && f.Source != repository.SourceCampaign && f.Source != repository.SourceAutomatic:
		return nil, 0, domain.InvalidField("source", "unknown")
	case f.Type != "" && !typePattern.MatchString(f.Type):
		return nil, 0, domain.InvalidField("type", "unknown")
	case f.Locale != "" && !oneOf(f.Locale, domain.Locales):
		return nil, 0, domain.InvalidField("locale", "unknown")
	}
	return s.repo.ListDeliveries(ctx, f)
}

func oneOf(v string, all []string) bool {
	for _, a := range all {
		if a == v {
			return true
		}
	}
	return false
}
