package repository

import (
	"context"
	"database/sql"
	"encoding/json"
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

// BankSubscriptionPlan uses the immutable mandate snapshot. Publishing a new
// Base bundle must not silently rewrite an already sold daily bank contract.
func (s *Store) BankSubscriptionPlan(ctx context.Context, subscriptionID, planID string) (domain.Plan, error) {
	plan, err := s.Plan(ctx, planID)
	if err != nil {
		return plan, err
	}
	var raw string
	if err = s.db.Reader().QueryRowContext(ctx, `SELECT snapshot_json FROM bcc_subscriptions WHERE id=?`, subscriptionID).Scan(&raw); err != nil {
		return plan, err
	}
	var sold struct {
		DailyLimit   int   `json:"daily_limit"`
		MonthlyLimit int   `json:"monthly_limit"`
		Unlimited    bool  `json:"unlimited"`
		Price        int64 `json:"price_minor"`
	}
	if err = json.Unmarshal([]byte(raw), &sold); err != nil {
		return plan, err
	}
	plan.DailyLimit, plan.MonthlyLimit, plan.Price = sold.DailyLimit, sold.MonthlyLimit, sold.Price
	plan.EntitlementKind = domain.EntitlementDaily
	if sold.Unlimited {
		plan.EntitlementKind = domain.EntitlementUnlimited
	} else if sold.MonthlyLimit > 0 {
		plan.EntitlementKind = domain.EntitlementMonthly
	}
	return plan, nil
}

func (s *Store) LegacyBaseDailyUnits(ctx context.Context) (int, error) {
	var units int
	err := s.db.Reader().QueryRowContext(ctx, `SELECT CAST(value AS INTEGER) FROM system_settings WHERE key='apple_base_legacy_daily_units'`).Scan(&units)
	return units, err
}
