package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// InsertAIReport — бір шағым. Мәтін тек адам оны жіберуді таңдаса келеді.
func (s *Store) InsertAIReport(ctx context.Context, r domain.AIReport) (domain.AIReport, error) {
	if r.ID == "" {
		r.ID = traits.NewID()
	}
	if r.Status == "" {
		r.Status = domain.ReportOpen
	}
	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO ai_reports (id, user_id, mode, reason, comment, text, platform, app_version, status, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.UserID, r.Mode, r.Reason, r.Comment, r.Text, r.Platform, r.AppVersion, r.Status, ms(r.CreatedAt))
	return r, err
}

// AIReportRow — әкімші тізіміндегі шағым: қолданушы мен шешкен әкімші қосылған.
type AIReportRow struct {
	Report domain.AIReport
	// User — шағымдаған тіркелгі (тек бүркемеленген идентификатор үшін).
	User domain.User
	// ResolvedByEmail — шешкен әкімшінің поштасы (әкімші өшірілсе бос).
	ResolvedByEmail string
}

// AIReports — шағымдар, жаңасы алдымен. status бос болса — бәрі.
func (s *Store) AIReports(ctx context.Context, status string, page traits.Page) ([]AIReportRow, int, error) {
	where, args := "1 = 1", []any{}
	if status != "" {
		where, args = "r.status = ?", append(args, status)
	}
	var total int
	if err := s.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ai_reports r WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT r.id, r.user_id, r.mode, r.reason, r.comment, r.text, r.platform, r.app_version, r.status,
		       r.created_at, r.resolved_at, r.resolved_by,
		       u.phone, u.email, u.legacy_client, COALESCE(a.email, '')
		FROM ai_reports r
		JOIN users u ON u.id = r.user_id
		LEFT JOIN admin_users a ON a.id = r.resolved_by
		WHERE `+where+`
		ORDER BY r.created_at DESC, r.id DESC LIMIT ? OFFSET ?`, append(args, page.Limit, page.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []AIReportRow
	for rows.Next() {
		var (
			row                   AIReportRow
			created               int64
			resolved              sql.NullInt64
			phone, email, legacyC sql.NullString
		)
		r := &row.Report
		if err := rows.Scan(&r.ID, &r.UserID, &r.Mode, &r.Reason, &r.Comment, &r.Text, &r.Platform, &r.AppVersion,
			&r.Status, &created, &resolved, &r.ResolvedBy, &phone, &email, &legacyC, &row.ResolvedByEmail); err != nil {
			return nil, 0, err
		}
		r.CreatedAt, r.ResolvedAt = timeFrom(created), timePtr(resolved)
		row.User = domain.User{ID: r.UserID, Phone: text(phone), Email: text(email), LegacyClient: text(legacyC)}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

// ResolveAIReport — шағымды шешілді деп белгілейді.
//
// changed=false: it was already resolved (nothing is rewritten, so the first
// administrator and time stay). ErrNotFound — no such report.
func (s *Store) ResolveAIReport(ctx context.Context, id, adminID string, at time.Time) (changed bool, err error) {
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE ai_reports SET status = ?, resolved_at = ?, resolved_by = ?
		WHERE id = ? AND status = ?`, domain.ReportResolved, ms(at), adminID, id, domain.ReportOpen)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return true, nil
	}
	var status string
	err = s.db.Reader().QueryRowContext(ctx, `SELECT status FROM ai_reports WHERE id = ?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, domain.ErrNotFound
	}
	return false, err
}
