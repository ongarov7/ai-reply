// Package reports — AI жасаған мәтінге шағымдар: қолданбадан қабылдау және әкімші тізімі.
//
// A report holds what the person chose to send: a reason, an optional comment
// and, only if they left the toggle on, the generated text. The copied message
// and the instruction never reach this package. Nothing from a report is
// written to the log.
package reports

import (
	"context"
	"log/slog"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Service — шағымдар.
type Service struct {
	repo  *repository.Store
	log   *slog.Logger
	clock traits.Clock
}

// New — қызмет.
func New(repo *repository.Store, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log, clock: traits.SystemClock{}}
}

// WithClock — тестке.
func (s *Service) WithClock(c traits.Clock) *Service { s.clock = c; return s }

// Input — қолданбадан келген шағым.
type Input struct {
	Mode       string
	Reason     string
	Comment    string
	Text       string
	Platform   string
	AppVersion string
}

// Create — шағымды тексеріп сақтайды.
//
// Unknown mode or reason and an oversized comment or text are refused with the
// field name (400 INVALID_REQUEST), never cut silently: a clipped reply could
// read differently from what the person reported.
func (s *Service) Create(ctx context.Context, userID string, in Input) (domain.AIReport, error) {
	report := domain.AIReport{
		UserID:     userID,
		Mode:       strings.TrimSpace(in.Mode),
		Reason:     strings.TrimSpace(in.Reason),
		Comment:    strings.TrimSpace(in.Comment),
		Text:       strings.TrimSpace(in.Text),
		Platform:   strings.ToLower(strings.TrimSpace(in.Platform)),
		AppVersion: traits.Clamp(strings.TrimSpace(in.AppVersion), 32),
		Status:     domain.ReportOpen,
		CreatedAt:  s.clock.Now(),
	}
	switch {
	case !traits.OneOf(report.Mode, domain.ReportModes...):
		return domain.AIReport{}, domain.InvalidField("mode", "reply or compose")
	case !traits.OneOf(report.Reason, domain.ReportReasons...):
		return domain.AIReport{}, domain.InvalidField("reason", "unknown reason")
	case traits.RuneLen(report.Comment) > domain.ReportCommentMax:
		return domain.AIReport{}, domain.InvalidField("comment", "at most 500 characters")
	case traits.RuneLen(report.Text) > domain.ReportTextMax:
		return domain.AIReport{}, domain.InvalidField("text", "at most 2000 characters")
	case report.Platform != "" && !traits.OneOf(report.Platform, domain.PlatformIOS, domain.PlatformAndroid):
		return domain.AIReport{}, domain.InvalidField("platform", "ios or android")
	}
	saved, err := s.repo.InsertAIReport(ctx, report)
	if err != nil {
		return domain.AIReport{}, err
	}
	// Metadata only: the comment and the text stay in the table.
	s.log.Info("ai report received", "report_id", saved.ID, "user_id", userID, "mode", saved.Mode,
		"reason", saved.Reason, "with_text", saved.Text != "", "platform", saved.Platform)
	return saved, nil
}

// List — әкімші тізімі, жаңасы алдымен. status: open | resolved | "" (бәрі).
func (s *Service) List(ctx context.Context, status string, page traits.Page) ([]repository.AIReportRow, int, error) {
	if status != "" && !traits.OneOf(status, domain.ReportStatuses...) {
		return nil, 0, domain.InvalidField("status", "unknown value")
	}
	return s.repo.AIReports(ctx, status, page)
}

// Resolve — шешілді деп белгілейді. changed=false: бұрын шешілген.
func (s *Service) Resolve(ctx context.Context, id, adminID string) (bool, error) {
	if strings.TrimSpace(id) == "" {
		return false, domain.ErrNotFound
	}
	return s.repo.ResolveAIReport(ctx, id, adminID, s.clock.Now())
}
