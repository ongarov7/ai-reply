package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// HasUsableUnlimitedSubscriptionInTx checks existing access, independently of renewal
// consent. Checkout calls it inside its writer transaction so a concurrent grant
// committed before the checkout cannot be missed. Bank access uses the sold
// snapshot, and Google keeps the same renewal bridge as ActiveGoogleSubscription.
func HasUsableUnlimitedSubscriptionInTx(ctx context.Context, tx *sql.Tx, user string, now time.Time) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM subscriptions s JOIN plans p ON p.id=s.plan_id
  WHERE s.user_id=:user AND s.status IN ('active','trial')
  AND (s.expires_at IS NULL OR s.expires_at>=:now)
  AND p.is_free=0 AND p.entitlement_kind='unlimited')
 OR EXISTS(SELECT 1 FROM apple_subscriptions a JOIN plans p ON p.id=a.plan_id
  WHERE a.user_id=:user AND a.revoked_at IS NULL AND a.expires_at>:now
  AND p.is_free=0 AND p.entitlement_kind='unlimited')
 OR EXISTS(SELECT 1 FROM google_subscriptions g JOIN plans p ON p.id=g.plan_id
  WHERE g.user_id=:user AND g.revoked_at IS NULL AND g.superseded_by=''
  AND g.state IN (:google_active,:google_grace,:google_canceled)
  AND (g.expires_at>:now OR (g.auto_renew=1 AND g.state<>:google_canceled
   AND g.read_at<g.expires_at AND g.expires_at>:google_bridge_start))
  AND p.is_free=0 AND p.entitlement_kind='unlimited')
 OR EXISTS(SELECT 1 FROM bcc_subscription_periods period
  JOIN bcc_subscriptions b ON b.id=period.subscription_id
  WHERE b.user_id=:user AND period.revoked_at IS NULL
  AND period.starts_at<=:now AND period.expires_at>:now
  AND json_extract(b.snapshot_json,'$.unlimited')=1)`,
		sql.Named("user", user), sql.Named("now", ms(now)),
		sql.Named("google_active", domain.GoogleSubActive), sql.Named("google_grace", domain.GoogleSubGrace),
		sql.Named("google_canceled", domain.GoogleSubCanceled), sql.Named("google_bridge_start", ms(now.Add(-googleRenewalBridge))),
	).Scan(&exists)
	return exists, err
}
