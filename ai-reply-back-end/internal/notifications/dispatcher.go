package notifications

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
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
)

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

// Run — жұмысшы: науқандарды таратады, жеткізулерді жібереді, науқандарды аяқтайды.
//
// Every backend process may run it: the claim is an atomic database update
// with a lease, so two workers never send the same delivery at the same time.
func (s *Service) Run(ctx context.Context) {
	if !s.cfg.Enabled || !s.cfg.WorkerEnabled {
		s.log.Info("push worker not started", "enabled", s.cfg.Enabled, "worker", s.cfg.WorkerEnabled)
		return
	}
	if len(s.providers) == 0 {
		s.log.Warn("push worker not started: no provider is configured (set the FIREBASE_* or APNS_* variables)")
		return
	}
	s.log.Info("push worker started", "owner", s.owner, "fcm", s.providers[domain.PlatformAndroid] != nil,
		"apns", s.providers[domain.PlatformIOS] != nil)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			s.log.Info("push worker stopped")
			return
		case <-s.wakeCh:
		case <-ticker.C:
		}
	}
}

// Tick — бір толық өту. Тестте жұмысшыны қолмен айналдыру үшін де қолданылады.
func (s *Service) Tick(ctx context.Context) {
	s.fanOutCampaigns(ctx)
	for i := 0; i < batchesPerTick && ctx.Err() == nil; i++ {
		n, err := s.ProcessBatch(ctx)
		if err != nil {
			s.log.Error("push batch failed", "error", err.Error())
			break
		}
		if n == 0 {
			break
		}
	}
	s.finishCampaigns(ctx)
}

func (s *Service) fanOutCampaigns(ctx context.Context) {
	ids, err := s.repo.QueuedCampaignIDs(ctx, 5)
	if err != nil {
		s.log.Error("queued campaigns lookup failed", "error", err.Error())
		return
	}
	for _, id := range ids {
		claimed, devices, users, err := s.repo.FanOutCampaign(ctx, id, s.query(s.clock.Now()))
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
		if err != nil || stats.Pending() > 0 {
			continue
		}
		status := domain.FinalCampaignStatus(stats)
		if done, err := s.repo.FinishCampaign(ctx, id, status, stats, s.clock.Now()); err == nil && done {
			s.log.Info("campaign finished", "event", "push_campaign_finished", "campaign_id", id,
				"status", status, "accepted", stats.Accepted, "failed", stats.Failed,
				"invalid_token", stats.InvalidToken, "skipped", stats.Skipped)
		}
	}
}

// ProcessBatch — бір топ жеткізуді алып, параллель жібереді. Нешеуін алғанын қайтарады.
func (s *Service) ProcessBatch(ctx context.Context) (int, error) {
	claimed, err := s.repo.ClaimDeliveries(ctx, s.owner, s.clock.Now(), leaseDuration, s.cfg.BatchSize)
	if err != nil || len(claimed) == 0 {
		return 0, err
	}
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
		n, err := s.repo.NotificationByID(ctx, id)
		if err == nil {
			cache[id] = n
		}
		return n, err
	}
	for _, d := range claimed {
		wg.Add(1)
		tokens <- struct{}{}
		go func(d repository.ClaimedDelivery) {
			defer wg.Done()
			defer func() { <-tokens }()
			s.deliver(ctx, d, notification)
		}(d)
	}
	wg.Wait()
	return len(claimed), nil
}

// deliver — бір жеткізу: тексеру, жіберу, нәтижені жазу.
func (s *Service) deliver(ctx context.Context, d repository.ClaimedDelivery, notification func(string) (domain.Notification, error)) {
	started := s.clock.Now()
	finish := func(status, code, detail, messageID string, next time.Time, fingerprint string, attemptMs int64) {
		ok, err := s.repo.FinishDelivery(ctx, d.ID, s.owner, repository.DeliveryOutcome{
			Status: status, NextAttemptAt: next, ProviderMessageID: messageID,
			ErrorCode: code, ErrorDetail: detail, Now: s.clock.Now(),
		})
		level := s.log.Info
		if status == domain.DeliveryFailed || status == domain.DeliveryRetrying {
			level = s.log.Warn
		}
		level("push delivery", "event", "push_delivery", "delivery_id", d.ID, "notification_id", d.NotificationID,
			"campaign_id", d.CampaignID, "installation_id", d.InstallationID, "user_id", d.UserID,
			"platform", d.Platform, "provider", d.Provider, "attempt", d.AttemptCount, "status", status,
			"error_code", code, "push", fingerprint, "duration_ms", attemptMs, "recorded", ok && err == nil)
		if err != nil {
			s.log.Error("push delivery result not saved", "delivery_id", d.ID, "error", err.Error())
		}
	}

	if d.AttemptCount > s.cfg.MaxAttempts {
		// The lease of an earlier attempt ran out with no recorded outcome.
		finish(domain.DeliveryFailed, "max_attempts", "attempts exhausted", "", started, "", 0)
		return
	}
	n, err := notification(d.NotificationID)
	if err != nil {
		finish(domain.DeliverySkipped, "notification_missing", "", "", started, "", 0)
		return
	}
	target, err := s.repo.InstallationSendTarget(ctx, d.InstallationID)
	if err != nil {
		finish(domain.DeliverySkipped, "installation_missing", "", "", started, "", 0)
		return
	}
	fingerprint := domain.TokenFingerprint(target.Provider, target.TokenHash)
	switch {
	case d.UserID != "" && target.UserID != d.UserID:
		// Signed out, or another account signed in on this phone since the
		// notification was queued: never show the previous account's message.
		finish(domain.DeliverySkipped, "recipient_changed", "", "", started, fingerprint, 0)
		return
	case target.PushStatus != domain.PushActive || target.TokenSealed == "":
		finish(domain.DeliverySkipped, "token_inactive", "", "", started, fingerprint, 0)
		return
	case !target.NotificationsEnabled:
		finish(domain.DeliverySkipped, "disabled_in_app", "", "", started, fingerprint, 0)
		return
	case !domain.PermissionAllowsAlerts(target.Permission):
		finish(domain.DeliverySkipped, "permission_denied", "", "", started, fingerprint, 0)
		return
	}
	provider := s.providers[target.Platform]
	if provider == nil || s.sealer == nil {
		finish(domain.DeliverySkipped, "provider_unavailable", "", "", started, fingerprint, 0)
		return
	}
	token, err := s.sealer.Open(target.TokenSealed)
	if err != nil {
		// The sealing key changed (JWT_ACCESS_SECRET rotated): the app sends
		// its token again on the next launch.
		_, _ = s.repo.InvalidateInstallationToken(ctx, target.InstallationID, target.TokenHash, "token_unreadable", s.clock.Now())
		finish(domain.DeliveryInvalidToken, "token_unreadable", "", "", started, fingerprint, 0)
		return
	}

	msg := push.Message{
		Title: n.Title, Body: n.Body, Category: n.Category, CollapseID: n.ID, TTL: DefaultTTL,
		Important: domain.CategoryImportant(n.Category), Data: payloadData(n, d.ID),
	}
	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	res := provider.Send(sendCtx, push.Target{Token: token, Environment: target.Environment}, msg)
	cancel()
	elapsed := s.clock.Now().Sub(started).Milliseconds()

	switch res.Outcome {
	case push.Accepted:
		finish(domain.DeliveryAccepted, "", "", res.MessageID, started, fingerprint, elapsed)
	case push.InvalidToken:
		if _, err := s.repo.InvalidateInstallationToken(ctx, target.InstallationID, target.TokenHash, res.Code, s.clock.Now()); err != nil {
			s.log.Error("token invalidation failed", "installation_id", target.InstallationID, "error", err.Error())
		} else {
			s.log.Info("push token deactivated", "event", "push_token_invalid", "installation_id", target.InstallationID,
				"platform", target.Platform, "error_code", res.Code, "push", fingerprint)
		}
		finish(domain.DeliveryInvalidToken, res.Code, res.Detail, "", started, fingerprint, elapsed)
	case push.Retry:
		if d.AttemptCount >= s.cfg.MaxAttempts {
			finish(domain.DeliveryFailed, res.Code, "retries exhausted: "+res.Detail, "", started, fingerprint, elapsed)
			return
		}
		next := s.clock.Now().Add(Backoff(d.AttemptCount, res.RetryAfter, s.jitter()))
		finish(domain.DeliveryRetrying, res.Code, res.Detail, "", next, fingerprint, elapsed)
	default:
		finish(domain.DeliveryFailed, res.Code, res.Detail, "", started, fingerprint, elapsed)
	}
}

// payloadData — екі платформа да алатын жалпақ кілттер.
func payloadData(n domain.Notification, deliveryID string) map[string]string {
	data := make(map[string]string, len(n.Data)+5)
	for k, v := range n.Data {
		data[k] = v
	}
	data["nid"] = n.ID
	data["did"] = deliveryID
	data["type"] = n.Type
	data["category"] = n.Category
	if n.Link != "" {
		data["link"] = n.Link
	}
	return data
}
