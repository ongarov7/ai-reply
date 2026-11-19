package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// AIContext — the account's AI context document as stored (0042_ai_context).
// domain.ErrNotFound: the account never saved one.
func (s *Store) AIContext(ctx context.Context, userID string) (string, int64, time.Time, error) {
	var (
		raw      string
		revision int64
		updated  int64
	)
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT document, revision, updated_at FROM ai_contexts WHERE user_id = ?`, userID).
		Scan(&raw, &revision, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, time.Time{}, domain.ErrNotFound
	}
	if err != nil {
		return "", 0, time.Time{}, err
	}
	return raw, revision, timeFrom(updated), nil
}

// SaveAIContext — replaces the document only if the stored revision is still
// expected (0: there is no row yet). One statement decides, so two devices
// saving at once cannot both win: the loser gets ok=false and the revision
// that is stored now.
func (s *Store) SaveAIContext(ctx context.Context, userID, raw string, expected int64, now time.Time) (bool, int64, error) {
	at := ms(now)
	var (
		ok      bool
		current int64
	)
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var res sql.Result
		var err error
		if expected == 0 {
			res, err = tx.ExecContext(ctx, `
				INSERT INTO ai_contexts (user_id, document, revision, created_at, updated_at)
				VALUES (?, ?, 1, ?, ?) ON CONFLICT (user_id) DO NOTHING`,
				userID, raw, at, at)
		} else {
			res, err = tx.ExecContext(ctx, `
				UPDATE ai_contexts SET document = ?, revision = revision + 1, updated_at = ?
				WHERE user_id = ? AND revision = ?`,
				raw, at, userID, expected)
		}
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		ok = n == 1
		err = tx.QueryRowContext(ctx, `SELECT revision FROM ai_contexts WHERE user_id = ?`, userID).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			current, err = 0, nil
		}
		return err
	})
	if err != nil {
		return false, 0, err
	}
	return ok, current, nil
}

// UsageUnitsByDay — replies charged per day (plan + extra buckets, the context
// surcharge included) for usage_date keys from..to inclusive.
func (s *Store) UsageUnitsByDay(ctx context.Context, userID, from, to string) (map[string]int, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT usage_date, used + extra_used FROM usage_daily
		WHERE user_id = ? AND usage_date >= ? AND usage_date <= ?`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var (
			date  string
			units int
		)
		if err := rows.Scan(&date, &units); err != nil {
			return nil, err
		}
		out[date] = units
	}
	return out, rows.Err()
}

// GenerationStamp — when and from which platform one successful reply or
// compose was made. Metadata only.
type GenerationStamp struct {
	At       time.Time
	Platform string
}

// SuccessfulGenerations — the account's delivered replies (screenshot replies
// included) and composed messages since a moment, oldest first
// (idx_ai_events_user).
func (s *Store) SuccessfulGenerations(ctx context.Context, userID string, since time.Time) ([]GenerationStamp, error) {
	// created_at is when the reply was delivered; minus its latency it is when
	// the request started and its units were charged (the usage_daily day), so
	// a reply that finishes after midnight stays on the day of its charge.
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT created_at - latency_ms, platform FROM ai_usage_events
		WHERE user_id = ? AND created_at >= ? AND status = 'success'
		  AND mode IN ('reply', 'compose', 'screenshot', 'screenshot_image')
		ORDER BY created_at`, userID, ms(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GenerationStamp
	for rows.Next() {
		var (
			at       int64
			platform string
		)
		if err := rows.Scan(&at, &platform); err != nil {
			return nil, err
		}
		out = append(out, GenerationStamp{At: timeFrom(at), Platform: platform})
	}
	return out, rows.Err()
}
