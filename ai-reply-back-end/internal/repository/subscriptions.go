package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

const subColumns = `id, user_id, plan_id, status, source, started_at, expires_at, cancelled_at, created_at, updated_at`

func scanSubscription(row interface{ Scan(...any) error }) (domain.Subscription, error) {
	var (
		s                         domain.Subscription
		started, created, updated int64
		expires, cancelled        sql.NullInt64
	)
	if err := row.Scan(&s.ID, &s.UserID, &s.PlanID, &s.Status, &s.Source, &started, &expires,
		&cancelled, &created, &updated); err != nil {
		return domain.Subscription{}, err
	}
	s.StartedAt, s.CreatedAt, s.UpdatedAt = timeFrom(started), timeFrom(created), timeFrom(updated)
	s.ExpiresAt, s.CancelledAt = timePtr(expires), timePtr(cancelled)
	return s, nil
}

// CurrentSubscription — қолданушының ағымдағы жазылымы (белсендісі басым).
func (s *Store) CurrentSubscription(ctx context.Context, userID string) (domain.Subscription, error) {
	sub, err := scanSubscription(s.db.Reader().QueryRowContext(ctx, `
		SELECT `+subColumns+` FROM subscriptions WHERE user_id = ?
		ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'trial' THEN 1 WHEN 'payment_pending' THEN 2 ELSE 3 END,
		         created_at DESC LIMIT 1`, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Subscription{}, domain.ErrNotFound
	}
	return sub, err
}

// CreateSubscription — жаңа жазылым.
func (s *Store) CreateSubscription(ctx context.Context, sub domain.Subscription) (domain.Subscription, error) {
	if sub.ID == "" {
		sub.ID = traits.NewID()
	}
	now := time.Now().UTC()
	if sub.StartedAt.IsZero() {
		sub.StartedAt = now
	}
	sub.CreatedAt, sub.UpdatedAt = now, now
	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO subscriptions (`+subColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		sub.ID, sub.UserID, sub.PlanID, sub.Status, sub.Source, ms(sub.StartedAt),
		msPtr(sub.ExpiresAt), msPtr(sub.CancelledAt), ms(now), ms(now))
	return sub, err
}

// ReplaceSubscription — ескісін жауып, жаңасын ашады (тарифті ауыстыру).
func (s *Store) ReplaceSubscription(ctx context.Context, userID string, next domain.Subscription) (domain.Subscription, error) {
	if next.ID == "" {
		next.ID = traits.NewID()
	}
	now := time.Now().UTC()
	if next.StartedAt.IsZero() {
		next.StartedAt = now
	}
	next.CreatedAt, next.UpdatedAt = now, now
	err := s.db.Tx(ctx, func(tx *sql.Tx) error { return replaceSubscription(ctx, tx, userID, next, now) })
	return next, err
}

// replaceSubscription — транзакция ішінде: белсенді жазылымдарды жауып, next-ті қосады.
func replaceSubscription(ctx context.Context, tx *sql.Tx, userID string, next domain.Subscription, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE subscriptions SET status = 'cancelled', cancelled_at = ?, updated_at = ?
		WHERE user_id = ? AND status IN ('active','trial','payment_pending')`,
		ms(now), ms(now), userID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO subscriptions (`+subColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		next.ID, userID, next.PlanID, next.Status, next.Source, ms(next.StartedAt),
		msPtr(next.ExpiresAt), nil, ms(now), ms(now))
	return err
}

// CompletePayment — төлемді «сәтті» деп белгілеп, тарифті бір транзакцияда ауыстырады.
//
// The payment status and the subscription change together or not at all, so
// "succeeded" always means the plan was given. applied=false: the payment was
// already succeeded (a parallel or earlier confirm finished it) and nothing
// was written.
func (s *Store) CompletePayment(ctx context.Context, paymentID, ref string, next domain.Subscription) (bool, domain.Subscription, error) {
	if next.ID == "" {
		next.ID = traits.NewID()
	}
	now := time.Now().UTC()
	if next.StartedAt.IsZero() {
		next.StartedAt = now
	}
	next.CreatedAt, next.UpdatedAt = now, now
	applied := false
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE payments SET status = 'succeeded', provider_ref = ?, updated_at = ?
			WHERE id = ? AND status <> 'succeeded'`, ref, ms(now), paymentID)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			return err
		}
		if err := replaceSubscription(ctx, tx, next.UserID, next, now); err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err != nil || !applied {
		return false, domain.Subscription{}, err
	}
	return true, next, nil
}

// UpdateSubscription — күй/мерзім өзгерісі.
func (s *Store) UpdateSubscription(ctx context.Context, sub domain.Subscription) error {
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE subscriptions SET plan_id = ?, status = ?, expires_at = ?, cancelled_at = ?, updated_at = ?
		WHERE id = ?`,
		sub.PlanID, sub.Status, msPtr(sub.ExpiresAt), msPtr(sub.CancelledAt), ms(time.Now()), sub.ID)
	return affected(res, err)
}

// ExpireDueSubscriptions — мерзімі өткендерін белгілейді (фон тапсырмасы).
func (s *Store) ExpireDueSubscriptions(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE subscriptions SET status = 'expired', updated_at = ?
		WHERE status IN ('active','trial') AND expires_at IS NOT NULL AND expires_at < ?`,
		ms(now), ms(now))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SubscriptionsByUser — тарих.
func (s *Store) SubscriptionsByUser(ctx context.Context, userID string) ([]domain.Subscription, error) {
	rows, err := s.db.Reader().QueryContext(ctx,
		`SELECT `+subColumns+` FROM subscriptions WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Subscription
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}
