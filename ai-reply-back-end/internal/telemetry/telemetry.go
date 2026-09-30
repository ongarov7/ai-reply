// Package telemetry — қосымша оқиғалары, кіру оқиғалары, API қателері және сақтау мерзімі.
//
// Three kinds of records with different durability:
//
//   - auth events (sign-in, codes, refresh, sign-out) are a security log:
//     written synchronously, one row per attempt, kept for a year by default;
//   - app events and sessions are best-effort analytics: validated against an
//     allowlist, queued in memory and written in batches, dropped (never
//     blocking) when the queue is full;
//   - API error records are written the same batched way by the access log.
//
// Nothing here may carry a message, a reply, a code, a token or a password:
// event names and their properties come from a fixed schema, values are
// short typed codes, and personal identifiers stay in the users table (rows
// point at user_id).
package telemetry

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Queue and batching limits.
const (
	queueSize      = 512
	flushEvery     = time.Second
	flushThreshold = 200
	janitorEvery   = 6 * time.Hour
	retentionBatch = 5000
)

// Service — телеметрия.
type Service struct {
	repo       *repository.Store
	cfg        config.Telemetry
	log        *slog.Logger
	clock      traits.Clock
	subjectKey []byte

	queue   chan repository.TelemetryBatch
	flushMu sync.Mutex
	dropped atomic.Int64
	started atomic.Bool
	done    chan struct{}
}

// New — қызмет. secret — субъект хэшінің кілт көзі (JWT_ACCESS_SECRET).
func New(repo *repository.Store, cfg config.Telemetry, secret string, log *slog.Logger) *Service {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("ai-reply/audit-subject/v1"))
	return &Service{
		repo: repo, cfg: cfg, log: log, clock: traits.SystemClock{}, subjectKey: mac.Sum(nil),
		queue: make(chan repository.TelemetryBatch, queueSize), done: make(chan struct{}),
	}
}

// WithClock — тестке арналған.
func (s *Service) WithClock(c traits.Clock) *Service { s.clock = c; return s }

// Enabled — қосымша оқиғалары қабылдана ма.
func (s *Service) Enabled() bool { return s.cfg.Enabled }

// SubjectHash — пошта не телефонның кілтпен хэші (кіру оқиғаларын іздеу үшін, мәннің өзінсіз).
func (s *Service) SubjectHash(kind, value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	mac := hmac.New(sha256.New, s.subjectKey)
	mac.Write([]byte(kind + ":" + value))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

// RecordAuth — кіру оқиғасы. Сәтсіздігі кіруді тоқтатпайды: тек журналға жазылады.
func (s *Service) RecordAuth(ctx context.Context, e domain.AuthEvent) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = s.clock.Now()
	}
	// The request may already be cancelled (the client hung up); the record
	// still belongs in the security log.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := s.repo.InsertAuthEvent(ctx, e); err != nil {
		s.log.Error("auth event not recorded", "event_name", e.Name, "error", err.Error())
	}
}

// RecordAPIError — сервер қайтарған қатенің метадерегі (кезек арқылы).
func (s *Service) RecordAPIError(e domain.APIError) {
	s.enqueue(repository.TelemetryBatch{APIErrors: []domain.APIError{e}})
}

func (s *Service) enqueue(b repository.TelemetryBatch) bool {
	select {
	case s.queue <- b:
		return true
	default:
		if n := s.dropped.Add(1); n == 1 || n%100 == 0 {
			s.log.Warn("telemetry queue full, batch dropped", "dropped_total", n)
		}
		return false
	}
}

// Start — фондағы жазушы және тазалаушы. ctx жабылғанда кезек соңына дейін жазылады.
func (s *Service) Start(ctx context.Context) {
	if !s.started.CompareAndSwap(false, true) {
		return
	}
	go s.writer(ctx)
	go s.janitor(ctx)
}

// Wait — жазушы кезекті босатып болғанша күтеді (сервер тоқтағанда).
func (s *Service) Wait(timeout time.Duration) {
	if !s.started.Load() {
		return
	}
	select {
	case <-s.done:
	case <-time.After(timeout):
		s.log.Warn("telemetry flush timed out")
	}
}

func (s *Service) writer(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()
	var pending repository.TelemetryBatch
	size := func() int {
		return len(pending.Events) + len(pending.APIErrors) + len(pending.Sessions) + len(pending.Opens)
	}
	for {
		select {
		case b := <-s.queue:
			merge(&pending, b)
			if size() >= flushThreshold {
				s.write(pending)
				pending = repository.TelemetryBatch{}
			}
		case <-ticker.C:
			if size() > 0 {
				s.write(pending)
				pending = repository.TelemetryBatch{}
			}
		case <-ctx.Done():
			for {
				select {
				case b := <-s.queue:
					merge(&pending, b)
				default:
					if size() > 0 {
						s.write(pending)
					}
					return
				}
			}
		}
	}
}

// Flush — кезектегінің бәрін қазір жазады (тесттер мен тоқтату үшін).
func (s *Service) Flush(ctx context.Context) {
	var pending repository.TelemetryBatch
	for {
		select {
		case b := <-s.queue:
			merge(&pending, b)
		default:
			if len(pending.Events)+len(pending.APIErrors)+len(pending.Sessions)+len(pending.Opens) > 0 {
				s.write(pending)
			}
			return
		}
	}
}

func merge(dst *repository.TelemetryBatch, src repository.TelemetryBatch) {
	dst.Events = append(dst.Events, src.Events...)
	dst.Sessions = append(dst.Sessions, src.Sessions...)
	dst.Opens = append(dst.Opens, src.Opens...)
	dst.APIErrors = append(dst.APIErrors, src.APIErrors...)
}

func (s *Service) write(b repository.TelemetryBatch) {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.repo.WriteTelemetry(ctx, b); err != nil {
		s.log.Error("telemetry batch not written", "events", len(b.Events), "api_errors", len(b.APIErrors),
			"error", err.Error())
	}
}

// ---------------------------------------------------------------- retention

func (s *Service) janitor(ctx context.Context) {
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.RunRetention(ctx)
			timer.Reset(janitorEvery)
		}
	}
}

// RunRetention — сақтау мерзімі өткен жолдарды өшіреді. 0 күн — ол түрі өшірілмейді.
func (s *Service) RunRetention(ctx context.Context) map[string]int64 {
	now := s.clock.Now()
	plan := []struct {
		table string
		days  int
	}{
		{"app_events", s.cfg.RetentionAppEventsDays},
		{"app_sessions", s.cfg.RetentionAppEventsDays},
		{"api_errors", s.cfg.RetentionAPIErrorsDays},
		{"auth_events", s.cfg.RetentionAuthEventsDays},
		{"notification_deliveries", s.cfg.RetentionDeliveriesDays},
		{"notifications", s.cfg.RetentionDeliveriesDays},
		{"admin_audit_logs", s.cfg.RetentionAuditLogDays},
	}
	out := map[string]int64{}
	for _, p := range plan {
		if p.days <= 0 {
			continue
		}
		n, err := s.repo.DeleteOlderThan(ctx, p.table, now.AddDate(0, 0, -p.days), retentionBatch)
		if err != nil {
			s.log.Error("retention failed", "table", p.table, "error", err.Error())
			continue
		}
		out[p.table] = n
		if n > 0 {
			s.log.Info("retention removed rows", "table", p.table, "rows", n, "older_than_days", p.days)
		}
	}
	return out
}
