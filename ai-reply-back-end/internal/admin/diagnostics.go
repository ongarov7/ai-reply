package admin

import (
	"context"

	"github.com/aireply/ai-reply-back-end/internal/repository"
)

// Diagnostics — диагностика хабарлаған орнатулар тізімі және әр сүзгінің саны.
func (s *Service) Diagnostics(ctx context.Context, f repository.DiagnosticsFilter) ([]repository.DiagnosticsRow, int, map[string]int, error) {
	return s.repo.ListDiagnostics(ctx, f)
}

// UserDiagnostics — тіркелгінің әр орнатуының диагностикасы және еске салу журналы.
func (s *Service) UserDiagnostics(ctx context.Context, userID string) ([]repository.DiagnosticsRow, []repository.ReminderState, error) {
	if _, err := s.repo.UserByID(ctx, userID); err != nil {
		return nil, nil, err
	}
	rows, err := s.repo.DiagnosticsForUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	states, err := s.repo.ReminderStates(ctx, userID)
	return rows, states, err
}

// SupportDiagnostics — the permission state of the account's devices for the
// support conversation page (support.DiagnosticsSource). The value is the
// []repository.DiagnosticsRow the user page shows; adminapi formats it.
func (s *Service) SupportDiagnostics(ctx context.Context, userID string) (any, error) {
	rows, err := s.repo.DiagnosticsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []repository.DiagnosticsRow{}
	}
	return rows, nil
}
