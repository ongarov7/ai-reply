// Package retention — ескі деректерді мерзімімен тазалау (RETENTION_* баптаулары).
//
// Account data lives until the account is deleted. What this job removes is
// what outlives its use: spent sign-in codes, dead refresh tokens, phones that
// never signed in again, and request and onboarding metadata older than the
// windows the privacy policy states. Notification rows have their own job
// (notifications.RunRetention).
package retention

import (
	"context"
	"log/slog"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

const (
	// interval — өту жиілігі; batch — бір DELETE өлшемі.
	interval = 6 * time.Hour
	batch    = 500
)

// Service — тазалау.
type Service struct {
	repo  *repository.Store
	cfg   config.Retention
	log   *slog.Logger
	clock traits.Clock
}

// New — қызмет.
func New(repo *repository.Store, cfg config.Retention, log *slog.Logger) *Service {
	return &Service{repo: repo, cfg: cfg, log: log, clock: traits.SystemClock{}}
}

// WithClock — тестке.
func (s *Service) WithClock(c traits.Clock) *Service { s.clock = c; return s }

// Result — бір өтуде өшірілген жолдар саны кесте бойынша.
type Result map[string]int64

// rule — бір кесте: мерзімі (күн, 0 — өшірілмейді) және өшіру сұранысы.
type rule struct {
	table  string
	days   int
	delete func(context.Context, time.Time, int) (int64, error)
}

func (s *Service) rules() []rule {
	return []rule{
		{"otp_codes", s.cfg.OTPDays, s.repo.DeleteOldOTPCodes},
		{"refresh_tokens", s.cfg.RefreshTokenDays, s.repo.DeleteDeadRefreshTokens},
		{"app_installations", s.cfg.AnonInstallationsDays, s.repo.DeleteStaleAnonymousInstallations},
		{"product_events", s.cfg.ProductEventsDays, s.repo.DeleteOldProductEvents},
		{"ai_usage_events", s.cfg.AIUsageEventsDays, s.repo.DeleteOldAIUsageEvents},
	}
}

// Sweep — бір өту. Бір кестенің қатесі қалғандарын тоқтатпайды.
func (s *Service) Sweep(ctx context.Context) Result {
	now := s.clock.Now()
	out := Result{}
	for _, r := range s.rules() {
		if r.days <= 0 {
			continue
		}
		n, err := r.delete(ctx, now.AddDate(0, 0, -r.days), batch)
		if err != nil {
			s.log.Error("data retention failed", "table", r.table, "error", err.Error())
		}
		if n > 0 {
			out[r.table] = n
		}
	}
	if len(out) > 0 {
		s.log.Info("data retention removed rows", "rows", map[string]int64(out))
	}
	return out
}

// Run — тәулігіне бірнеше рет Sweep. ctx тоқтағанда қайтады.
func (s *Service) Run(ctx context.Context) {
	active := false
	for _, r := range s.rules() {
		active = active || r.days > 0
	}
	if !active {
		s.log.Info("data retention off: every RETENTION_* window is 0")
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.Sweep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
