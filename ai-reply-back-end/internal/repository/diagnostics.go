package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

const diagnosticsColumns = `d.installation_id, d.keyboard_setup_status, d.keyboard_selected_status, d.full_access_status,
	d.clipboard_status, d.onboarding_status, d.status_source, d.last_successful_paste_at, d.last_keyboard_check_at,
	d.keyboard_verified_at, d.full_access_verified_at, d.clipboard_denied_at, d.onboarding_completed_at,
	d.reported_at, d.created_at, d.updated_at`

// diagnosticsScan — diagnosticsColumns (LEFT JOIN-да жол жоқ болса value() nil береді).
type diagnosticsScan struct {
	id                                                sql.NullString
	keyboard, selected, fullAccess, clipboard, onb    sql.NullString
	source                                            sql.NullString
	paste, check, kbVerified, faVerified, denied, onc sql.NullInt64
	reported, created, updated                        sql.NullInt64
}

func (d *diagnosticsScan) dest() []any {
	return []any{&d.id, &d.keyboard, &d.selected, &d.fullAccess, &d.clipboard, &d.onb, &d.source,
		&d.paste, &d.check, &d.kbVerified, &d.faVerified, &d.denied, &d.onc, &d.reported, &d.created, &d.updated}
}

func (d *diagnosticsScan) value() *domain.InstallationDiagnostics {
	if !d.id.Valid {
		return nil
	}
	return &domain.InstallationDiagnostics{
		InstallationID: d.id.String, KeyboardSetupStatus: d.keyboard.String, KeyboardSelectedStatus: d.selected.String,
		FullAccessStatus: d.fullAccess.String, ClipboardStatus: d.clipboard.String, OnboardingStatus: d.onb.String,
		StatusSource: d.source.String, LastSuccessfulPasteAt: timePtr(d.paste), LastKeyboardCheckAt: timePtr(d.check),
		KeyboardVerifiedAt: timePtr(d.kbVerified), FullAccessVerifiedAt: timePtr(d.faVerified),
		ClipboardDeniedAt: timePtr(d.denied), OnboardingCompletedAt: timePtr(d.onc),
		ReportedAt: timeFrom(d.reported.Int64), CreatedAt: timeFrom(d.created.Int64), UpdatedAt: timeFrom(d.updated.Int64),
	}
}

// DiagnosticsReport — қосымша хабарлаған күй (сервис тексеріп, уақыттарды қысқан).
type DiagnosticsReport struct {
	InstallationID         string // the app-generated id
	UserID                 string // the caller; "" — anonymous
	KeyboardSetupStatus    string
	KeyboardSelectedStatus string
	FullAccessStatus       string
	ClipboardStatus        string
	OnboardingStatus       string
	StatusSource           string
	LastSuccessfulPasteAt  *time.Time
	LastKeyboardCheckAt    *time.Time
	Now                    time.Time
}

// ErrInstallationOwner — орнату басқа тіркелгіге тіркелген.
var ErrInstallationOwner = domain.ConflictField("installation_id", "attached to another account")

// UpsertInstallationDiagnostics — орнатудың диагностикасын жазады, бір транзакцияда.
//
// The installation must exist and must not be attached to another account
// (an anonymous caller may write only an anonymous installation). The
// verified timestamps are sticky: the first verification stays. A verified
// success on an attached installation resolves that account's reminder of
// the same kind for good, in the same transaction.
func (s *Store) UpsertInstallationDiagnostics(ctx context.Context, in DiagnosticsReport) (domain.InstallationDiagnostics, error) {
	var out domain.InstallationDiagnostics
	now := ms(in.Now)
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var id string
		var owner sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT id, user_id FROM app_installations WHERE installation_id = ?`,
			in.InstallationID).Scan(&id, &owner)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return err
		}
		if text(owner) != in.UserID {
			return ErrInstallationOwner
		}
		at := func(cond bool) any {
			if cond {
				return now
			}
			return nil
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO installation_diagnostics (installation_id, keyboard_setup_status, keyboard_selected_status,
				full_access_status, clipboard_status, onboarding_status, status_source, last_successful_paste_at,
				last_keyboard_check_at, keyboard_verified_at, full_access_verified_at, clipboard_denied_at,
				onboarding_completed_at, reported_at, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT (installation_id) DO UPDATE SET
				keyboard_setup_status = excluded.keyboard_setup_status,
				keyboard_selected_status = excluded.keyboard_selected_status,
				full_access_status = excluded.full_access_status,
				clipboard_status = excluded.clipboard_status,
				onboarding_status = excluded.onboarding_status,
				status_source = excluded.status_source,
				last_successful_paste_at = CASE
					WHEN excluded.last_successful_paste_at IS NULL THEN last_successful_paste_at
					WHEN last_successful_paste_at IS NULL THEN excluded.last_successful_paste_at
					ELSE MAX(last_successful_paste_at, excluded.last_successful_paste_at) END,
				last_keyboard_check_at = COALESCE(excluded.last_keyboard_check_at, last_keyboard_check_at),
				keyboard_verified_at = COALESCE(keyboard_verified_at, excluded.keyboard_verified_at),
				full_access_verified_at = COALESCE(full_access_verified_at, excluded.full_access_verified_at),
				clipboard_denied_at = CASE
					WHEN excluded.clipboard_status <> 'observed_denied' THEN clipboard_denied_at
					WHEN clipboard_status = 'observed_denied' AND clipboard_denied_at IS NOT NULL THEN clipboard_denied_at
					ELSE excluded.clipboard_denied_at END,
				onboarding_completed_at = COALESCE(onboarding_completed_at, excluded.onboarding_completed_at),
				reported_at = excluded.reported_at,
				updated_at = excluded.updated_at`,
			id, in.KeyboardSetupStatus, in.KeyboardSelectedStatus, in.FullAccessStatus, in.ClipboardStatus,
			in.OnboardingStatus, in.StatusSource, msPtr(in.LastSuccessfulPasteAt), msPtr(in.LastKeyboardCheckAt),
			at(in.KeyboardSetupStatus == domain.DiagVerifiedEnabled),
			at(in.FullAccessStatus == domain.DiagVerifiedEnabled),
			at(in.ClipboardStatus == domain.DiagObservedDenied),
			at(in.OnboardingStatus == domain.OnboardingCompleted),
			now, now, now)
		if err != nil {
			return err
		}
		row := diagnosticsScan{}
		if err := tx.QueryRowContext(ctx, `SELECT `+diagnosticsColumns+`
			FROM installation_diagnostics d WHERE d.installation_id = ?`, id).Scan(row.dest()...); err != nil {
			return err
		}
		out = *row.value()
		if in.UserID == "" {
			return nil
		}
		var resolved []string
		if out.KeyboardVerifiedAt != nil {
			resolved = append(resolved, domain.TypeKeyboardSetupIncomplete)
		}
		if out.ClipboardStatus == domain.DiagVerifiedEnabled {
			resolved = append(resolved, domain.TypeClipboardHelp)
		}
		if out.OnboardingCompletedAt != nil {
			resolved = append(resolved, domain.TypeOnboardingIncomplete)
		}
		return resolveReminders(ctx, tx, in.UserID, resolved, now)
	})
	return out, err
}

// resolveReminders — еске салуды біржола жабады (дәлелденген сәттен кейін қайта жіберілмейді).
func resolveReminders(ctx context.Context, tx *sql.Tx, userID string, types []string, now int64) error {
	for _, kind := range types {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO notification_reminders (user_id, type, resolved_at, updated_at) VALUES (?,?,?,?)
			ON CONFLICT (user_id, type) DO UPDATE SET resolved_at = excluded.resolved_at, updated_at = excluded.updated_at
			WHERE notification_reminders.resolved_at IS NULL`, userID, kind, now, now); err != nil {
			return err
		}
	}
	return nil
}

// DiagnosticsRow — орнату, оның диагностикасы (жоқ болса nil) және тіркелгінің идентификаторы.
type DiagnosticsRow struct {
	Installation domain.Installation
	Diagnostics  *domain.InstallationDiagnostics
	UserEmail    string
	UserPhone    string
}

// DiagnosticsForUser — тіркелгінің барлық орнатуы, диагностикасымен (хабарламаған — nil).
func (s *Store) DiagnosticsForUser(ctx context.Context, userID string) ([]DiagnosticsRow, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT `+prefixColumns(installationColumns, "i.")+`, `+diagnosticsColumns+`
		FROM app_installations i LEFT JOIN installation_diagnostics d ON d.installation_id = i.id
		WHERE i.user_id = ? ORDER BY i.last_seen_at DESC, i.id LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DiagnosticsRow
	for rows.Next() {
		d := diagnosticsScan{}
		inst, err := scanInstallation(rows, d.dest()...)
		if err != nil {
			return nil, err
		}
		out = append(out, DiagnosticsRow{Installation: inst, Diagnostics: d.value()})
	}
	return out, rows.Err()
}

// diagnosticsFilterClause — сүзгі чипінің бекітілген SQL үзіндісі (d — диагностика, i — орнату).
func diagnosticsFilterClause(filter string) string {
	switch filter {
	case domain.DiagFilterSetupIncomplete:
		return `d.onboarding_status IN ('not_started','in_progress')`
	case domain.DiagFilterKeyboardNotVerified:
		return `d.keyboard_setup_status IN ('not_verified','observed_denied','user_reported_disabled')`
	case domain.DiagFilterPasteProblems:
		return `d.clipboard_status IN ('observed_denied','user_reported_disabled')`
	case domain.DiagFilterFullAccessDisabled:
		return `d.full_access_status IN ('observed_denied','user_reported_disabled')`
	case domain.DiagFilterNotificationsDisabled:
		return `(i.notifications_enabled = 0 OR i.push_permission = 'denied')`
	case domain.DiagFilterUnknown:
		return `d.keyboard_setup_status = 'unknown'`
	default:
		return "1=1"
	}
}

// DiagnosticsFilter — әкімші тізімі.
type DiagnosticsFilter struct {
	Filter   string // domain.DiagnosticFilters, "" — all
	Platform string
	Search   string // user id prefix, device model, short installation id
	Page     traits.Page
}

// ListDiagnostics — диагностика хабарлаған орнатулар, соңғы хабарлағаны алдымен, және әр сүзгінің саны.
func (s *Store) ListDiagnostics(ctx context.Context, f DiagnosticsFilter) ([]DiagnosticsRow, int, map[string]int, error) {
	where := []string{"1=1"}
	args := []any{}
	if f.Platform != "" {
		where = append(where, "i.platform = ?")
		args = append(args, f.Platform)
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		like := escapeLike(q)
		conditions := []string{`i.user_id LIKE ? ESCAPE '\'`, `i.device_model LIKE ? ESCAPE '\'`}
		args = append(args, like+"%", "%"+like+"%")
		if len(q) <= shortInstallationID {
			conditions = append(conditions, `i.installation_id LIKE ? ESCAPE '\'`)
			args = append(args, like+"%")
		}
		where = append(where, "("+strings.Join(conditions, " OR ")+")")
	}
	base := strings.Join(where, " AND ")
	from := ` FROM installation_diagnostics d JOIN app_installations i ON i.id = d.installation_id
		LEFT JOIN users u ON u.id = i.user_id WHERE `

	counts := map[string]int{}
	sums := []string{"COUNT(*)"}
	for _, name := range domain.DiagnosticFilters {
		sums = append(sums, "COALESCE(SUM(CASE WHEN "+diagnosticsFilterClause(name)+" THEN 1 ELSE 0 END), 0)")
	}
	values := make([]int, len(sums))
	dest := make([]any, len(sums))
	for i := range values {
		dest[i] = &values[i]
	}
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT `+strings.Join(sums, ", ")+from+base, args...).
		Scan(dest...); err != nil {
		return nil, 0, nil, err
	}
	counts["all"] = values[0]
	for i, name := range domain.DiagnosticFilters {
		counts[name] = values[i+1]
	}

	clause := base + " AND " + diagnosticsFilterClause(f.Filter)
	var total int
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*)`+from+clause, args...).Scan(&total); err != nil {
		return nil, 0, nil, err
	}
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT `+prefixColumns(installationColumns, "i.")+`, `+diagnosticsColumns+`, COALESCE(u.email, ''), COALESCE(u.phone, '')`+
		from+clause+` ORDER BY d.reported_at DESC, d.installation_id LIMIT ? OFFSET ?`,
		append(args, f.Page.Limit, f.Page.Offset)...)
	if err != nil {
		return nil, 0, nil, err
	}
	defer rows.Close()
	var out []DiagnosticsRow
	for rows.Next() {
		d := diagnosticsScan{}
		row := DiagnosticsRow{}
		inst, err := scanInstallation(rows, append(d.dest(), &row.UserEmail, &row.UserPhone)...)
		if err != nil {
			return nil, 0, nil, err
		}
		row.Installation, row.Diagnostics = inst, d.value()
		out = append(out, row)
	}
	return out, total, counts, rows.Err()
}
