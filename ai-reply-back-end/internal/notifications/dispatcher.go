package notifications

import (
	"context"
	"errors"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/email"
	"github.com/aireply/ai-reply-back-end/internal/push"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

// Жұмысшы баптаулары.
const (
	// leaseDuration — жол жұмысшыда қанша тұрады. A batch of sends finishes far
	// sooner; a worker that dies mid-send frees its rows when this runs out.
	leaseDuration = 5 * time.Minute
	sendTimeout   = 15 * time.Second
	pollInterval  = 2 * time.Second
	// batchesPerTick — one pass sends at most this many batches, then looks at
	// campaigns again, so a large campaign does not starve anything else.
	batchesPerTick = 20
	// retentionInterval — ескі жолдарды тазалау жиілігі; retentionBatch — бір DELETE өлшемі.
	retentionInterval = 6 * time.Hour
	retentionBatch    = 500
	// dedupeHorizon — an automatic notification row is the once-per-period
	// guard of its event, and the longest period is a calendar month
	// (quota_*:month keys): the row outlives a shorter retention.
	dedupeHorizon = MinNotificationRetentionDays * 24 * time.Hour
)

// MinNotificationRetentionDays — хабарлама жолы кемінде осынша күн сақталады
// (RETENTION_NOTIFICATIONS_DAYS одан қысқа болса да). The privacy policy
// prints it next to the shorter delivery window.
const MinNotificationRetentionDays = 40

// retrySchedule — сәтсіз әрекеттен кейінгі күту: 2-ші әрекет ~5 с, 3-ші ~30 с,
// 4-ші ~2 мин, 5-ші ~10 мин (±20% кездейсоқ ауытқумен).
var retrySchedule = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute}

// Backoff — attempt-ші әрекет сәтсіз болғаннан кейін қанша күту керек.
//
// The provider's own Retry-After wins when it asks for longer. Jitter keeps a
// burst of failures from coming back as a burst.
func Backoff(attempt int, hint time.Duration, jitter float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	idx := attempt - 1
	if idx >= len(retrySchedule) {
		idx = len(retrySchedule) - 1
	}
	wait := retrySchedule[idx]
	if hint > wait {
		wait = hint
	}
	if wait > time.Hour {
		wait = time.Hour
	}
	// jitter ∈ [0,1) → factor ∈ [0.8, 1.2)
	return time.Duration(float64(wait) * (0.8 + 0.4*jitter))
}

func randomJitter() float64 { return rand.Float64() }

// Run — жұмысшы: науқандарды таратады, push пен хаттарды жібереді, науқандарды аяқтайды.
//
// Every backend process may run it: the claim is an atomic database update
// with a lease, so two workers never send the same delivery at the same time.
// It returns when ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	mailer, _ := s.email()
	if !s.cfg.WorkerEnabled {
		s.log.Info("notification worker not started", "worker", false)
		return
	}
	if !s.Ready() && mailer == nil {
		s.log.Info("notification worker not started: neither push nor notification e-mail is configured",
			"push_enabled", s.cfg.Enabled, "fcm", s.provider != nil)
		return
	}
	s.log.Info("notification worker started", "owner", s.owner, "push", s.Ready(), "notification_email", mailer != nil)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			s.log.Info("notification worker stopped")
			return
		case <-s.wakeCh:
		case <-ticker.C:
		}
	}
}

// Tick — бір толық өту. Тестте жұмысшыны қолмен айналдыру үшін де қолданылады.
func (s *Service) Tick(ctx context.Context) {
	if s.Ready() {
		s.fanOutCampaigns(ctx)
	}
	for i := 0; i < batchesPerTick && ctx.Err() == nil; i++ {
		n, err := s.ProcessBatch(ctx)
		if err != nil {
			s.log.Error("notification batch failed", "error", err.Error())
			break
		}
		if n == 0 {
			break
		}
	}
	if ctx.Err() != nil {
		return // stopping: the next pass, here or in another process, closes campaigns
	}
	s.finishCampaigns(ctx)
	s.keepCancelledStats(ctx)
}

func (s *Service) fanOutCampaigns(ctx context.Context) {
	ids, err := s.repo.QueuedCampaignIDs(ctx, 5)
	if err != nil {
		s.log.Error("queued campaigns lookup failed", "error", err.Error())
		return
	}
	for _, id := range ids {
		campaign, err := s.repo.Campaign(ctx, id)
		if err != nil {
			s.log.Error("campaign lookup failed", "campaign_id", id, "error", err.Error())
			continue
		}
		q, _, err := s.audienceQuery(ctx, campaign.Audience, campaign.Category)
		if err != nil {
			s.log.Error("campaign audience failed", "campaign_id", id, "error", err.Error())
			continue
		}
		texts := make(map[string]domain.LocalizedText, len(domain.Locales))
		for _, l := range domain.Locales {
			texts[l], _ = campaign.TextFor(l)
		}
		claimed, devices, users, err := s.repo.FanOutCampaign(ctx, campaign, q, texts)
		if err != nil {
			s.log.Error("campaign fan-out failed", "campaign_id", id, "error", err.Error())
			continue
		}
		if claimed {
			s.log.Info("campaign fanned out", "event", "push_campaign_started", "campaign_id", id,
				"devices", devices, "users", users)
		}
	}
}

func (s *Service) finishCampaigns(ctx context.Context) {
	ids, err := s.repo.ProcessingCampaignIDs(ctx, 20)
	if err != nil {
		s.log.Error("processing campaigns lookup failed", "error", err.Error())
		return
	}
	for _, id := range ids {
		stats, err := s.repo.CampaignStats(ctx, id)
		if err != nil {
			s.log.Error("campaign stats failed", "campaign_id", id, "error", err.Error())
			continue
		}
		if stats.Pending() > 0 {
			continue
		}
		status := domain.FinalCampaignStatus(stats)
		done, err := s.repo.FinishCampaign(ctx, id, status, stats, s.clock.Now())
		if err != nil {
			s.log.Error("campaign finish failed", "campaign_id", id, "error", err.Error())
			continue
		}
		if done {
			s.log.Info("campaign finished", "event", "push_campaign_finished", "campaign_id", id,
				"status", status, "accepted", stats.Accepted, "failed", stats.Failed,
				"invalid_token", stats.InvalidToken, "skipped", stats.Skipped)
		}
	}
}

// keepCancelledStats — тоқтатылған науқанның есебі де сақталады (соңғы жіберу жазылғаннан кейін),
// өйткені оның жеткізулерін де сақтау мерзімі кейін өшіреді.
func (s *Service) keepCancelledStats(ctx context.Context) {
	ids, err := s.repo.CancelledCampaignsWithoutStats(ctx, 20)
	if err != nil {
		s.log.Error("cancelled campaigns lookup failed", "error", err.Error())
		return
	}
	for _, id := range ids {
		stats, err := s.repo.CampaignStats(ctx, id)
		if err != nil {
			s.log.Error("campaign stats failed", "campaign_id", id, "error", err.Error())
			continue
		}
		if stats.Pending() > 0 {
			continue
		}
		if _, err := s.repo.SaveCancelledCampaignStats(ctx, id, stats, s.clock.Now()); err != nil {
			s.log.Error("campaign stats not saved", "campaign_id", id, "error", err.Error())
		}
	}
}

// ProcessBatch — бір топ жеткізуді алып, параллель жібереді. Нешеуін алғанын қайтарады.
//
// When ctx is cancelled (the server is stopping) no new send starts: the
// claimed rows not started yet go back to the queue, and the sends already
// under way finish and save their result, so a push the provider accepted is
// not sent again after the restart.
func (s *Service) ProcessBatch(ctx context.Context) (int, error) {
	claimed, err := s.repo.ClaimDeliveries(ctx, s.owner, s.clock.Now(), leaseDuration, s.cfg.BatchSize)
	if err != nil || len(claimed) == 0 {
		return 0, err
	}
	work := context.WithoutCancel(ctx)
	var (
		mu     sync.Mutex
		cache  = map[string]domain.Notification{}
		wg     sync.WaitGroup
		tokens = make(chan struct{}, s.cfg.Concurrency)
	)
	notification := func(id string) (domain.Notification, error) {
		mu.Lock()
		defer mu.Unlock()
		if n, ok := cache[id]; ok {
			return n, nil
		}
		n, err := s.repo.NotificationByID(work, id)
		if err == nil {
			cache[id] = n
		}
		return n, err
	}
	for i, d := range claimed {
		select {
		case tokens <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			s.release(work, claimed[i:])
			break
		}
		wg.Add(1)
		go func(d repository.ClaimedDelivery) {
			defer wg.Done()
			defer func() { <-tokens }()
			s.deliver(work, d, notification)
		}(d)
	}
	wg.Wait()
	return len(claimed), nil
}

// release — тоқтап жатқан жұмысшы бастамаған жолдарды кезекке қайтарады.
func (s *Service) release(ctx context.Context, rows []repository.ClaimedDelivery) {
	ids := make([]string, 0, len(rows))
	for _, d := range rows {
		ids = append(ids, d.ID)
	}
	if err := s.repo.ReleaseDeliveries(ctx, s.owner, ids, s.clock.Now()); err != nil {
		s.log.Error("claimed deliveries not released", "count", len(ids), "error", err.Error())
	}
}

// outcome — бір әрекеттің нәтижесі (журнал мен дерекқорға).
type outcome struct {
	status, code, detail, messageID string
	next                            time.Time
	fingerprint                     string
	durationMs                      int64
}

// deliver — бір жеткізу: тексеру, жіберу, нәтижені жазу.
func (s *Service) deliver(ctx context.Context, d repository.ClaimedDelivery, notification func(string) (domain.Notification, error)) {
	started := s.clock.Now()
	var o outcome
	switch {
	case d.AttemptCount > s.cfg.MaxAttempts:
		// The lease of an earlier attempt ran out with no recorded outcome.
		o = outcome{status: domain.DeliveryFailed, code: "max_attempts", detail: "attempts exhausted"}
	default:
		n, err := notification(d.NotificationID)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			o = outcome{status: domain.DeliverySkipped, code: "notification_missing"}
		case err != nil:
			// A failed read (a busy or broken connection) is tried again later.
			s.log.Warn("notification lookup failed", "delivery_id", d.ID, "error", err.Error())
			o.status, o.next = s.retry(d, 0)
			o.code = "notification_lookup_failed"
		case d.Channel == domain.ChannelEmail:
			o = s.sendEmail(ctx, d, n, started)
		default:
			o = s.sendPush(ctx, d, n, started)
		}
	}
	if o.next.IsZero() {
		o.next = started
	}
	stored, err := s.repo.FinishDelivery(ctx, d.ID, s.owner, repository.DeliveryOutcome{
		Status: o.status, NextAttemptAt: o.next, ProviderMessageID: o.messageID, TokenFingerprint: o.fingerprint,
		ErrorCode: o.code, ErrorDetail: o.detail, Now: s.clock.Now(),
	})
	status := o.status
	if stored != "" {
		status = stored // a retry of a campaign cancelled meanwhile is stored as cancelled
	}
	level := s.log.Info
	if status == domain.DeliveryFailed || status == domain.DeliveryRetrying {
		level = s.log.Warn
	}
	level("notification delivery", "event", "notification_delivery", "delivery_id", d.ID,
		"notification_id", d.NotificationID, "campaign_id", d.CampaignID, "channel", d.Channel,
		"installation_id", d.InstallationID, "user_id", d.UserID, "platform", d.Platform, "provider", d.Provider,
		"attempt", d.AttemptCount, "status", status, "error_code", o.code, "push", o.fingerprint,
		"duration_ms", o.durationMs, "recorded", stored != "")
	if err != nil {
		s.log.Error("delivery result not saved", "delivery_id", d.ID, "error", err.Error())
	}
}

// sendPush — бір құрылғыға FCM арқылы (Android және iOS).
func (s *Service) sendPush(ctx context.Context, d repository.ClaimedDelivery, n domain.Notification, started time.Time) outcome {
	if d.InstallationID == "" {
		return outcome{status: domain.DeliverySkipped, code: "installation_missing"}
	}
	target, err := s.repo.InstallationSendTarget(ctx, d.InstallationID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return outcome{status: domain.DeliverySkipped, code: "installation_missing"}
	case err != nil:
		s.log.Warn("installation lookup failed", "delivery_id", d.ID, "error", err.Error())
		status, next := s.retry(d, 0)
		return outcome{status: status, next: next, code: "installation_lookup_failed"}
	}
	fingerprint := domain.TokenFingerprint(target.Provider, target.TokenHash)
	skip := func(code string) outcome {
		return outcome{status: domain.DeliverySkipped, code: code, fingerprint: fingerprint}
	}
	switch {
	case target.UserID == "" || target.UserID != d.UserID:
		// Signed out, or another account signed in on this phone since the
		// notification was queued: never show the previous account's message.
		return skip("recipient_changed")
	case target.PushStatus != domain.PushActive || target.TokenSealed == "":
		return skip("token_inactive")
	case !target.NotificationsEnabled:
		return skip("disabled_in_app")
	case !domain.PermissionAllowsAlerts(target.Permission):
		return skip("permission_denied")
	case !s.Ready() || s.sealer == nil:
		return skip("provider_unavailable")
	}
	token, err := s.sealer.Open(target.TokenSealed)
	if err != nil {
		// The sealing key changed (JWT_ACCESS_SECRET rotated): the app sends
		// its token again on the next launch.
		if _, err := s.repo.InvalidateInstallationToken(ctx, target.InstallationID, target.TokenHash, "token_unreadable", false, s.clock.Now()); err != nil {
			s.log.Error("token invalidation failed", "installation_id", target.InstallationID, "error", err.Error())
		}
		return outcome{status: domain.DeliveryInvalidToken, code: "token_unreadable", fingerprint: fingerprint}
	}

	msg := push.Message{
		Title: n.Title, Body: n.Body, Category: n.Category, CollapseID: n.ID, TTL: DefaultTTL,
		Important: domain.CategoryImportant(n.Category), Data: payloadData(n, d.ID),
	}
	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	res := s.provider.Send(sendCtx, push.Target{Token: token}, msg)
	cancel()
	o := outcome{code: res.Code, detail: res.Detail, fingerprint: fingerprint,
		durationMs: s.clock.Now().Sub(started).Milliseconds()}

	switch res.Outcome {
	case push.Accepted:
		o.status, o.code, o.detail, o.messageID = domain.DeliveryAccepted, "", "", res.MessageID
	case push.InvalidToken:
		// UNREGISTERED and a malformed token are final for that token.
		// SENDER_ID_MISMATCH can be this server's Firebase project instead:
		// once that is fixed the same token works, so it may come back.
		final := res.Code != "SENDER_ID_MISMATCH"
		if _, err := s.repo.InvalidateInstallationToken(ctx, target.InstallationID, target.TokenHash, res.Code, final, s.clock.Now()); err != nil {
			s.log.Error("token invalidation failed", "installation_id", target.InstallationID, "error", err.Error())
		} else {
			s.log.Info("push token deactivated", "event", "push_token_invalid", "installation_id", target.InstallationID,
				"platform", target.Platform, "error_code", res.Code, "push", fingerprint)
		}
		o.status = domain.DeliveryInvalidToken
	case push.Retry:
		o.status, o.next = s.retry(d, res.RetryAfter)
		if o.status == domain.DeliveryFailed {
			o.detail = "retries exhausted: " + res.Detail
		}
	default:
		o.status = domain.DeliveryFailed
	}
	return o
}

// sendEmail — тіркелгінің расталған поштасына бір хат (мекенжай жіберу сәтінде оқылады).
func (s *Service) sendEmail(ctx context.Context, d repository.ClaimedDelivery, n domain.Notification, started time.Time) outcome {
	mailer, renderer := s.email()
	if mailer == nil {
		return outcome{status: domain.DeliverySkipped, code: SkipEmailDisabled}
	}
	if d.UserID == "" {
		return outcome{status: domain.DeliverySkipped, code: "recipient_missing"}
	}
	recipient, err := s.repo.NotificationEmailRecipient(ctx, d.UserID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return outcome{status: domain.DeliverySkipped, code: "recipient_missing"}
	case err != nil:
		status, next := s.retry(d, 0)
		return outcome{status: status, next: next, code: "recipient_lookup_failed"}
	case !recipient.Active:
		return outcome{status: domain.DeliverySkipped, code: SkipRecipientExcluded}
	case recipient.Email == "" || !recipient.Verified:
		// Only an address the person proved they own: no mail to a typo.
		return outcome{status: domain.DeliverySkipped, code: "no_verified_email"}
	}
	content, err := renderer.Render(n.Type, n.Locale, n.Params)
	if errors.Is(err, email.ErrTemplateMissing) {
		return outcome{status: domain.DeliverySkipped, code: "email_template_missing"}
	}
	if err != nil {
		return outcome{status: domain.DeliveryFailed, code: "render_failed", detail: safeDetail(err.Error())}
	}
	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	err = mailer.Send(sendCtx, email.Outgoing{
		To: recipient.Email, Content: content, IdempotencyKey: "notification-" + d.ID, Reference: d.ID,
	})
	cancel()
	o := outcome{durationMs: s.clock.Now().Sub(started).Milliseconds()}
	if err == nil {
		o.status = domain.DeliveryAccepted
		return o
	}
	var rejected *email.DeliveryError
	if errors.As(err, &rejected) && rejected.Status >= 400 && rejected.Status < 500 && rejected.Status != 429 {
		o.status, o.code = domain.DeliveryFailed, "HTTP_"+strconv.Itoa(rejected.Status)
		o.detail = safeDetail(rejected.Name)
		return o
	}
	o.code, o.detail = "EMAIL_UNAVAILABLE", "the e-mail provider did not accept the message"
	if errors.As(err, &rejected) {
		o.code = "HTTP_" + strconv.Itoa(rejected.Status)
	}
	o.status, o.next = s.retry(d, 0)
	if o.status == domain.DeliveryFailed {
		o.detail = "retries exhausted"
	}
	return o
}

// retry — келесі әрекет уақыты, не әрекеттер біткенде provider_failed.
func (s *Service) retry(d repository.ClaimedDelivery, hint time.Duration) (string, time.Time) {
	if d.AttemptCount >= s.cfg.MaxAttempts {
		return domain.DeliveryFailed, time.Time{}
	}
	return domain.DeliveryRetrying, s.clock.Now().Add(Backoff(d.AttemptCount, hint, s.jitter()))
}

// payloadData — екі платформа да алатын жалпақ кілттер (params ешқашан кірмейді).
func payloadData(n domain.Notification, deliveryID string) map[string]string {
	data := make(map[string]string, len(n.Data)+5)
	for k, v := range n.Data {
		data[k] = v
	}
	data["nid"] = n.ID
	data["did"] = deliveryID
	data["type"] = n.Type
	data["category"] = n.Category
	data["link"] = n.Link
	return data
}

// safeDetail — қысқа, бір жолды мәтін журнал мен әкімші панеліне.
func safeDetail(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160])
	}
	return s
}

// ---------------------------------------------------------------- retention

// RunRetention — RETENTION_NOTIFICATIONS_DAYS-тан ескі аяқталған жолдарды тәулігіне бірнеше рет өшіреді.
// It returns when ctx is cancelled.
func (s *Service) RunRetention(ctx context.Context) {
	if s.cfg.RetentionDays <= 0 {
		s.log.Info("notification retention off: RETENTION_NOTIFICATIONS_DAYS=0")
		return
	}
	ticker := time.NewTicker(retentionInterval)
	defer ticker.Stop()
	for {
		s.Retain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Retain — бір өту. Кезектегі, жіберіліп жатқан және қайталанатын жеткізулер ешқашан өшпейді;
// науқанның қорытынды есебі науқан жолында қалады.
func (s *Service) Retain(ctx context.Context) (deliveries, notifications int64) {
	if s.cfg.RetentionDays <= 0 {
		return 0, 0
	}
	cutoff := s.clock.Now().AddDate(0, 0, -s.cfg.RetentionDays)
	deliveries, err := s.repo.DeleteFinishedDeliveries(ctx, cutoff, retentionBatch)
	if err != nil {
		s.log.Error("notification retention failed", "table", "notification_deliveries", "error", err.Error())
		return deliveries, 0
	}
	notificationCutoff := cutoff
	if guard := s.clock.Now().Add(-dedupeHorizon); guard.Before(notificationCutoff) {
		notificationCutoff = guard
	}
	notifications, err = s.repo.DeleteOrphanNotifications(ctx, notificationCutoff, retentionBatch)
	if err != nil {
		s.log.Error("notification retention failed", "table", "notifications", "error", err.Error())
	}
	if deliveries > 0 || notifications > 0 {
		s.log.Info("notification retention removed rows", "deliveries", deliveries, "notifications", notifications,
			"older_than_days", s.cfg.RetentionDays)
	}
	return deliveries, notifications
}
