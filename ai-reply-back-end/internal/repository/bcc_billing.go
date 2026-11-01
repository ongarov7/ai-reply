package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// ActiveBankSubscription returns the strongest still-paid bank entitlement.
// Canceling renewal does not remove paid access; refunded periods are excluded.
func (s *Store) ActiveBankSubscription(ctx context.Context, user string, now time.Time) (domain.Subscription, error) {
	var sub domain.Subscription
	var start, end int64
	var renew bool
	err := s.db.Reader().QueryRowContext(ctx, `SELECT b.id,b.user_id,b.plan_id,p.starts_at,p.expires_at,b.status='active'
 FROM bcc_subscription_periods p JOIN bcc_subscriptions b ON b.id=p.subscription_id JOIN plans plan ON plan.id=b.plan_id
 WHERE b.user_id=? AND p.revoked_at IS NULL AND p.starts_at<=? AND p.expires_at>?
 ORDER BY plan.entitlement_kind='unlimited' DESC,plan.daily_message_limit DESC,p.expires_at DESC LIMIT 1`, user, now.UnixMilli(), now.UnixMilli()).Scan(&sub.ID, &sub.UserID, &sub.PlanID, &start, &end, &renew)
	if errors.Is(err, sql.ErrNoRows) {
		return sub, domain.ErrNotFound
	}
	if err != nil {
		return sub, err
	}
	sub.Status, sub.Source, sub.StartedAt = domain.SubActive, "bcc", time.UnixMilli(start)
	expiry := time.UnixMilli(end)
	sub.ExpiresAt = &expiry
	sub.AutoRenews = &renew
	return sub, nil
}
