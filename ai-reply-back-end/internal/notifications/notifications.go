// Package notifications — push хабарламаларының қызметі: жасау, кезек, жеткізу, науқандар.
//
// Business code calls NotifyUser; the admin panel creates campaigns. Both
// write a logical notification and its per-device deliveries (the outbox) in
// one database transaction, keyed so that the same event or the same admin
// command can never produce a second notification. The dispatcher (Run) then
// claims deliveries with a lease, sends them through the FCM or APNs
// provider, retries transient failures with backoff and switches off tokens
// the provider reports dead.
package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/installations"
	"github.com/aireply/ai-reply-back-end/internal/push"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// StaleAfter — осыдан ұзақ көрінбеген орнатуға жіберілмейді (FCM 270 күннен кейін токенді өшіреді).
const StaleAfter = 270 * 24 * time.Hour

// DefaultTTL — провайдер жеткізілмеген хабарламаны қанша сақтайды.
const DefaultTTL = 24 * time.Hour

// Deps — тәуелділіктер.
type Deps struct {
	Repo          *repository.Store
	Installations *installations.Service
	// Providers — platform ("android", "ios") → provider. Only configured ones.
	Providers map[string]push.Provider
	Config    config.Push
	Location  *time.Location
	Log       *slog.Logger
	Clock     traits.Clock
}

// Service — хабарламалар.
type Service struct {
	repo      *repository.Store
	sealer    *installations.Sealer
	providers map[string]push.Provider
	cfg       config.Push
	loc       *time.Location
	log       *slog.Logger
	clock     traits.Clock
	owner     string
	wakeCh    chan struct{}
	jitter    func() float64
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
	providers := map[string]push.Provider{}
	for platform, p := range d.Providers {
		if p != nil {
			providers[platform] = p
		}
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
	host, _ := os.Hostname()
	var sealer *installations.Sealer
	if d.Installations != nil {
		sealer = d.Installations.Sealer()
	}
	return &Service{
		repo: d.Repo, sealer: sealer, providers: providers, cfg: cfg, loc: loc,
		log: d.Log, clock: clock, wakeCh: make(chan struct{}, 1), jitter: randomJitter,
		owner: fmt.Sprintf("%s:%d:%s", traits.Clamp(host, 32), os.Getpid(), traits.RandomToken(3)),
	}
}

// Status — әкімші панеліне: не бапталған.
type Status struct {
	Enabled   bool     `json:"enabled"`
	Worker    bool     `json:"worker"`
	FCM       bool     `json:"fcm"`
	APNs      bool     `json:"apns"`
	LinkHosts []string `json:"link_hosts"`
}

// Status — ағымдағы күй.
func (s *Service) Status() Status {
	return Status{
		Enabled:   s.cfg.Enabled,
		Worker:    s.cfg.Enabled && s.cfg.WorkerEnabled,
		FCM:       s.providers[domain.PlatformAndroid] != nil,
		APNs:      s.providers[domain.PlatformIOS] != nil,
		LinkHosts: s.cfg.LinkHosts,
	}
}

// Ready — хабарлама жіберуге бола ма (қосулы және кемінде бір провайдер бар).
func (s *Service) Ready() bool { return s.cfg.Enabled && len(s.providers) > 0 }

func (s *Service) platforms() []string {
	out := make([]string, 0, len(s.providers))
	for _, p := range []string{domain.PlatformAndroid, domain.PlatformIOS} {
		if s.providers[p] != nil {
			out = append(out, p)
		}
	}
	return out
}

func (s *Service) query(now time.Time) repository.AudienceQuery {
	return repository.AudienceQuery{
		Now: now, Platforms: s.platforms(), StaleAfter: StaleAfter, Location: s.loc,
	}
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
type UserNotification struct {
	UserID string
	// IdempotencyKey — the event's own identity, e.g. "payment_success:<payment id>".
	// The same key for the same user never produces a second notification.
	IdempotencyKey string
	Type           string // payment_success, subscription_expiring, …
	Category       string // domain.Category*
	Title          string
	Body           string
	Link           string            // aireply://<screen> or an allowed https URL
	Data           map[string]string // custom keys for the app
}

// NotifyResult — NotifyUser нәтижесі.
type NotifyResult struct {
	NotificationID string
	Created        bool // false: this event was already turned into a notification
	Devices        int  // deliveries queued now
	Skipped        string
}

var (
	typePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`)
	keyPattern  = regexp.MustCompile(`^[A-Za-z0-9:_.@-]{1,160}$`)
)

// NotifyUser — бизнес-оқиға → бір логикалық хабарлама → қолданушының әр құрылғысына жеткізу.
//
// Safe to call more than once for the same event (a retried request, a
// repeated sweep, two backend processes): the second call finds the first
// notification and queues nothing. With push switched off it does nothing.
func (s *Service) NotifyUser(ctx context.Context, n UserNotification) (NotifyResult, error) {
	if !s.Ready() {
		return NotifyResult{Skipped: "push_disabled"}, nil
	}
	if strings.TrimSpace(n.UserID) == "" {
		return NotifyResult{}, domain.InvalidField("user_id", "required")
	}
	if !keyPattern.MatchString(n.IdempotencyKey) {
		return NotifyResult{}, domain.InvalidField("idempotency_key", "format")
	}
	if !typePattern.MatchString(n.Type) {
		return NotifyResult{}, domain.InvalidField("type", "format")
	}
	content, err := s.validateContent(Content{
		Title: n.Title, Body: n.Body, Category: n.Category, Link: n.Link, Data: n.Data,
	})
	if err != nil {
		return NotifyResult{}, err
	}
	now := s.clock.Now()
	q := s.query(now)
	q.UserID, q.Category = n.UserID, content.Category
	id, created, devices, err := s.repo.CreateUserNotification(ctx, domain.Notification{
		DedupeKey:      "user:" + n.UserID + ":" + n.IdempotencyKey,
		IdempotencyKey: n.IdempotencyKey,
		UserID:         n.UserID,
		Category:       content.Category,
		Type:           n.Type,
		Title:          content.Title,
		Body:           content.Body,
		Link:           content.Link,
		Data:           content.Data,
	}, q)
	if err != nil {
		return NotifyResult{}, err
	}
	s.log.Info("notification created", "event", "push_notification_created",
		"notification_id", id, "user_id", n.UserID, "type", n.Type, "category", content.Category,
		"created", created, "devices", devices)
	if created && devices > 0 {
		s.wake()
	}
	return NotifyResult{NotificationID: id, Created: created, Devices: devices}, nil
}

// ---------------------------------------------------------------- preferences

// Preferences — қолданушының санаттар бойынша баптауы (әр санат үшін нақты мән).
func (s *Service) Preferences(ctx context.Context, userID string) (map[string]bool, error) {
	stored, err := s.repo.NotificationPreferences(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, c := range domain.NotificationCategories {
		enabled, ok := stored[c]
		out[c] = !ok || enabled || !domain.CategoryOptional(c)
	}
	return out, nil
}

// SetPreferences — тек өшіруге болатын санаттар өзгереді.
func (s *Service) SetPreferences(ctx context.Context, userID string, prefs map[string]bool) (map[string]bool, error) {
	clean := map[string]bool{}
	for category, enabled := range prefs {
		if !domain.IsNotificationCategory(category) {
			return nil, domain.InvalidField("preferences."+category, "unknown category")
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

// ---------------------------------------------------------------- campaigns

// CampaignInput — әкімші формасы.
type CampaignInput struct {
	Name     string
	Title    string
	Body     string
	Category string
	Link     string
	Data     map[string]string
	Audience domain.AudienceFilter
}

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_:.-]{8,128}$`)

// CreateCampaign — науқан жасайды (send=true болса, бірден кезекке қояды).
//
// The Idempotency-Key makes the command safe to repeat: a double click, a
// retried request after a timeout, or a reload that re-sends the form all
// return the campaign the first request created. created=false says so.
func (s *Service) CreateCampaign(ctx context.Context, adminID string, in CampaignInput, idempotencyKey string, send bool) (domain.Campaign, bool, error) {
	if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		return domain.Campaign{}, false, domain.InvalidField("idempotency_key", "send an Idempotency-Key header")
	}
	content, err := s.validateContent(Content{
		Title: in.Title, Body: in.Body, Category: in.Category, Link: in.Link, Data: in.Data, Campaign: true,
	})
	if err != nil {
		return domain.Campaign{}, false, err
	}
	audience, err := ValidateAudience(in.Audience)
	if err != nil {
		return domain.Campaign{}, false, err
	}
	name := traits.Clamp(traits.CollapseSpaces(in.Name), 120)
	if name == "" {
		name = traits.Clamp(content.Title, 120)
	}
	if send && !s.Ready() {
		return domain.Campaign{}, false, domain.ErrPushDisabled
	}
	requested := domain.Campaign{
		Name: name, Title: content.Title, Body: content.Body, Category: content.Category,
		Link: content.Link, Data: content.Data, Audience: audience, Status: domain.CampaignDraft,
		CreatedBy: adminID, IdempotencyKey: idempotencyKey, CreatedAt: s.clock.Now(),
	}
	campaign, created, err := s.repo.CreateCampaign(ctx, requested)
	if err != nil {
		return domain.Campaign{}, false, err
	}
	// A repeated key is a retry of the same form. A key reused for different
	// content is a client bug: answering with the first campaign would hide it.
	if !created && campaignFingerprint(campaign) != campaignFingerprint(requested) {
		return domain.Campaign{}, false, domain.ConflictField("idempotency_key", "already used for a different campaign")
	}
	if send {
		campaign, err = s.SendCampaign(ctx, campaign.ID)
	}
	return campaign, created, err
}

// campaignFingerprint — what a retried create must repeat exactly (content and audience).
func campaignFingerprint(c domain.Campaign) string {
	keys := make([]string, 0, len(c.Data))
	for k := range c.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, part := range []string{c.Name, c.Title, c.Body, c.Category, c.Link} {
		b.WriteString(strconv.Quote(part))
		b.WriteByte('|')
	}
	for _, k := range keys {
		b.WriteString(strconv.Quote(k) + "=" + strconv.Quote(c.Data[k]) + ";")
	}
	audience, _ := json.Marshal(c.Audience) // every field is omitempty: nil and empty agree
	b.WriteByte('|')
	b.Write(audience)
	return b.String()
}

// SendCampaign — draft → queued. Қайта шақыру зиянсыз: кезектегі не жіберілген науқан өзгермейді.
func (s *Service) SendCampaign(ctx context.Context, id string) (domain.Campaign, error) {
	campaign, err := s.repo.Campaign(ctx, id)
	if err != nil {
		return domain.Campaign{}, err
	}
	switch campaign.Status {
	case domain.CampaignDraft:
		if !s.Ready() {
			return domain.Campaign{}, domain.ErrPushDisabled
		}
		if _, err := s.repo.QueueCampaign(ctx, id, s.clock.Now()); err != nil {
			return domain.Campaign{}, err
		}
		s.log.Info("campaign queued", "event", "push_campaign_queued", "campaign_id", id)
		s.wake()
		return s.repo.Campaign(ctx, id)
	case domain.CampaignCancelled:
		return domain.Campaign{}, domain.ErrConflict
	default:
		return campaign, nil
	}
}

// CancelCampaign — әлі жіберілмегенін тоқтатады. Аяқталған науқанды тоқтату — қақтығыс.
func (s *Service) CancelCampaign(ctx context.Context, id string) (domain.Campaign, error) {
	campaign, err := s.repo.Campaign(ctx, id)
	if err != nil {
		return domain.Campaign{}, err
	}
	switch campaign.Status {
	case domain.CampaignCancelled:
		return campaign, nil
	case domain.CampaignDraft, domain.CampaignQueued, domain.CampaignProcessing:
		if _, err := s.repo.CancelCampaign(ctx, id, s.clock.Now()); err != nil {
			return domain.Campaign{}, err
		}
		s.log.Info("campaign cancelled", "event", "push_campaign_cancelled", "campaign_id", id)
		return s.repo.Campaign(ctx, id)
	default:
		return domain.Campaign{}, domain.ErrConflict
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
	// Deliveries of old campaigns are removed by retention; the counters
	// saved when the campaign finished remain.
	if stats.Total == 0 && c.FinalStats != nil {
		stats = *c.FinalStats
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

// Preview — сүзгіге сай алушылар саны. Ешнәрсе жіберілмейді.
func (s *Service) Preview(ctx context.Context, filter domain.AudienceFilter, category string) (repository.AudiencePreview, domain.AudienceFilter, error) {
	audience, err := ValidateAudience(filter)
	if err != nil {
		return repository.AudiencePreview{}, audience, err
	}
	if category == "" {
		category = domain.CategoryMarketing
	}
	if !domain.IsNotificationCategory(category) {
		return repository.AudiencePreview{}, audience, domain.InvalidField("category", "unknown")
	}
	q := s.query(s.clock.Now())
	q.Filter, q.Category = audience, category
	preview, err := s.repo.PreviewAudience(ctx, q)
	return preview, audience, err
}

// Deliveries — жеткізулер тізімі (әкімші).
func (s *Service) Deliveries(ctx context.Context, f repository.DeliveryFilter) ([]repository.DeliveryRow, int, error) {
	if f.Status != "" && !oneOf(f.Status, domain.DeliveryStatuses) {
		return nil, 0, domain.InvalidField("status", "unknown")
	}
	if f.Platform != "" && f.Platform != domain.PlatformAndroid && f.Platform != domain.PlatformIOS {
		return nil, 0, domain.InvalidField("platform", "unknown")
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
