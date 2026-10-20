package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// ReminderMark — еске салу хабарламасымен бірге журналға жазылатын белгі.
//
// Expected is the send count the sweep saw: the ledger moves to Expected+1
// only if it still holds Expected (compare-and-swap in the notification's
// transaction), so two processes can never send the same attempt twice.
type ReminderMark struct {
	Type     string
	Expected int
	Source   string // auto | admin
}

// Еске салу журналының көзі.
const (
	ReminderSourceAuto  = "auto"
	ReminderSourceAdmin = "admin"
)

// errReminderNotSent — еске салу жазылмады: құрылғы жоқ не журнал басқа процесте өзгерді (rollback).
var errReminderNotSent = errors.New("reminder not sent")

// markReminder — журналды CAS-пен жылжытады; өзгермесе errReminderNotSent (бүкіл транзакция кері қайтады).
func markReminder(ctx context.Context, tx *sql.Tx, userID, notificationID string, m ReminderMark, now int64) error {
	var res sql.Result
	var err error
	if m.Expected == 0 {
		res, err = tx.ExecContext(ctx, `
			INSERT INTO notification_reminders (user_id, type, send_count, last_sent_at, last_notification_id, last_source, updated_at)
			VALUES (?,?,1,?,?,?,?)
			ON CONFLICT (user_id, type) DO UPDATE SET send_count = notification_reminders.send_count + 1,
				last_sent_at = excluded.last_sent_at, last_notification_id = excluded.last_notification_id,
				last_source = excluded.last_source, updated_at = excluded.updated_at
			WHERE notification_reminders.send_count = 0 AND notification_reminders.resolved_at IS NULL`,
			userID, m.Type, now, notificationID, m.Source, now)
	} else {
		res, err = tx.ExecContext(ctx, `
			UPDATE notification_reminders SET send_count = send_count + 1, last_sent_at = ?, last_notification_id = ?,
				last_source = ?, updated_at = ?
			WHERE user_id = ? AND type = ? AND send_count = ? AND resolved_at IS NULL`,
			now, notificationID, m.Source, now, userID, m.Type, m.Expected)
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errReminderNotSent
	}
	return nil
}

// ReminderQuery — бір түрдің еске салу үміткерлері.
type ReminderQuery struct {
	Type          string
	Now           time.Time
	Cooldown      time.Duration
	MaxSends      int
	MinAccountAge time.Duration
	// Reach — who push can reach now (category system): an account with no
	// reachable device is not a candidate, so it uses no attempt.
	Reach AudienceQuery
	// After — the last user id of the previous page ("" — from the start).
	After string
	Limit int
}

// ReminderCandidate — еске салуға лайық тіркелгі және журналдағы жіберу саны.
type ReminderCandidate struct {
	UserID    string
	SendCount int
}

// freshEvidence — тек жаңа (≤ 14 күн), қосымша не пернетақта тексерген диагностика дәлел бола алады.
const freshEvidence = `d.reported_at >= ? AND d.status_source IN ('app_check','keyboard_check')`

// reminderEvidence — түрдің дәлел шарты (u — тіркелгі). Бір аргумент: freshness cutoff.
//
// Unknown, stale or absent diagnostics are no evidence. A verified success
// on any of the account's installations stops the reminder (the verified
// timestamps are sticky).
func reminderEvidence(kind string) string {
	evidence := func(condition string) string {
		return `EXISTS (SELECT 1 FROM installation_diagnostics d JOIN app_installations i ON i.id = d.installation_id
			WHERE i.user_id = u.id AND ` + freshEvidence + ` AND ` + condition + `)`
	}
	none := func(condition string) string {
		return `NOT EXISTS (SELECT 1 FROM installation_diagnostics d JOIN app_installations i ON i.id = d.installation_id
			WHERE i.user_id = u.id AND ` + condition + `)`
	}
	switch kind {
	case domain.TypeOnboardingIncomplete:
		return evidence(`d.onboarding_status IN ('not_started','in_progress')`) +
			` AND ` + none(`(d.onboarding_completed_at IS NOT NULL OR d.onboarding_status IN ('completed','skipped'))`) +
			` AND NOT EXISTS (SELECT 1 FROM user_profiles p WHERE p.user_id = u.id AND p.onboarding_completed = 1)`
	case domain.TypeKeyboardSetupIncomplete:
		return evidence(`d.keyboard_setup_status IN ('not_verified','observed_denied','user_reported_disabled')`) +
			` AND ` + none(`d.keyboard_verified_at IS NOT NULL`)
	case domain.TypeClipboardHelp:
		return evidence(`d.clipboard_status = 'observed_denied' AND d.clipboard_denied_at IS NOT NULL
			AND (d.last_successful_paste_at IS NULL OR d.last_successful_paste_at < d.clipboard_denied_at)`)
	default:
		return "0 = 1"
	}
}

// ReminderCandidates — дәлелі бар, әлі шешілмеген, шегі мен үзілісі өткен тіркелгілер.
func (s *Store) ReminderCandidates(ctx context.Context, q ReminderQuery) ([]ReminderCandidate, error) {
	if !domain.IsReminderType(q.Type) || q.Type == domain.TypeAccountInfo {
		return nil, domain.InvalidField("type", "unknown")
	}
	fresh := ms(q.Now.Add(-domain.DiagnosticsFreshness))
	reach, reachArgs := reachableClause(q.Reach)
	args := []any{q.Type, ms(q.Now.Add(-q.MinAccountAge)), q.MaxSends, ms(q.Now.Add(-q.Cooldown)), q.After, fresh, fresh}
	args = append(append(args, reachArgs...), q.Limit)
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT u.id, COALESCE(r.send_count, 0)
		FROM users u LEFT JOIN notification_reminders r ON r.user_id = u.id AND r.type = ?
		WHERE u.kind = 'account' AND u.status = 'active' AND u.deleted_at IS NULL AND u.created_at <= ?
		  AND r.resolved_at IS NULL AND COALESCE(r.send_count, 0) < ?
		  AND (r.last_sent_at IS NULL OR r.last_sent_at <= ?) AND u.id > ?
		  AND u.id IN (SELECT i.user_id FROM installation_diagnostics d JOIN app_installations i ON i.id = d.installation_id
		               WHERE i.user_id IS NOT NULL AND `+freshEvidence+`)
		  AND `+reminderEvidence(q.Type)+`
		  AND EXISTS (SELECT 1 FROM app_installations i WHERE i.user_id = u.id AND `+reach+`)
		ORDER BY u.id LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReminderCandidate
	for rows.Next() {
		var c ReminderCandidate
		if err := rows.Scan(&c.UserID, &c.SendCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ReminderState — журналдың бір жолы (әкімші панеліне).
type ReminderState struct {
	Type       string     `json:"type"`
	SendCount  int        `json:"send_count"`
	LastSentAt *time.Time `json:"-"`
	LastSource string     `json:"last_source"`
	ResolvedAt *time.Time `json:"-"`
}

// ReminderStates — тіркелгінің еске салу журналы.
func (s *Store) ReminderStates(ctx context.Context, userID string) ([]ReminderState, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT type, send_count, last_sent_at, last_source, resolved_at
		FROM notification_reminders WHERE user_id = ? ORDER BY type`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReminderState
	for rows.Next() {
		var r ReminderState
		var sent, resolved sql.NullInt64
		if err := rows.Scan(&r.Type, &r.SendCount, &sent, &r.LastSource, &resolved); err != nil {
			return nil, err
		}
		r.LastSentAt, r.ResolvedAt = timePtr(sent), timePtr(resolved)
		out = append(out, r)
	}
	return out, rows.Err()
}

// LatestInstallationTimezone — тіркелгінің соңғы көрінген құрылғысының уақыт белдеуі ("" — белгісіз).
func (s *Store) LatestInstallationTimezone(ctx context.Context, userID string) (string, error) {
	var tz string
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT timezone FROM app_installations WHERE user_id = ? AND timezone <> ''
		ORDER BY last_seen_at DESC, id LIMIT 1`, userID).Scan(&tz)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return tz, err
}
