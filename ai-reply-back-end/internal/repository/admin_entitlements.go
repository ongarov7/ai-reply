package repository

import "time"

// adminEntitlementsCTE resolves the plan once per nondeleted account without
// accruing credits or changing store state. Keep the provider selection and
// quota rules in step with subscriptions.Service.Entitlement: legacy first,
// then Apple, Google and bank, with the later provider winning equal access.
// In particular, bank access uses its sold snapshot, and Play keeps the same
// short renewal bridge as ActiveGoogleSubscription.
//
// Its first two bind arguments are the current time and Play's bridge cutoff.
const adminEntitlementsCTE = `WITH RECURSIVE
admin_clock(now, bridge_cutoff) AS (VALUES (?, ?)),
admin_default_plan AS (
 SELECT p.id FROM plans p
 WHERE p.code = COALESCE((SELECT NULLIF(value, '') FROM system_settings WHERE key='default_plan_code'), 'free')
    OR (p.is_free=1 AND p.is_active=1 AND p.archived_at IS NULL)
 ORDER BY CASE WHEN p.code = COALESCE((SELECT NULLIF(value, '') FROM system_settings WHERE key='default_plan_code'), 'free') THEN 0 ELSE 1 END,
          p.sort_order, p.price, p.id LIMIT 1
),
admin_legacy AS (
 SELECT s.*, ROW_NUMBER() OVER (PARTITION BY s.user_id
  ORDER BY CASE s.status WHEN 'active' THEN 0 WHEN 'trial' THEN 1 WHEN 'payment_pending' THEN 2 ELSE 3 END,
           s.created_at DESC, s.id) AS position
 FROM subscriptions s
),
admin_initial AS (
 SELECT u.id AS user_id, s.status, s.source, s.expires_at,
  s.status IN ('active','trial') AND (s.expires_at IS NULL OR s.expires_at>=clock.now)
   AND legacy_plan.id IS NOT NULL AS usable,
  CASE WHEN s.status IN ('active','trial') AND (s.expires_at IS NULL OR s.expires_at>=clock.now)
         AND legacy_plan.id IS NOT NULL THEN s.plan_id ELSE (SELECT id FROM admin_default_plan) END AS plan_id
 FROM users u CROSS JOIN admin_clock clock
 LEFT JOIN admin_legacy s ON s.user_id=u.id AND s.position=1
 LEFT JOIN plans legacy_plan ON legacy_plan.id=s.plan_id
 WHERE u.deleted_at IS NULL
),
admin_candidates AS (
 SELECT initial.user_id, initial.user_id || ':initial' AS candidate_id, 0 AS provider_order, 1 AS position,
  p.id AS plan_id, p.code, p.name_en, p.is_free,
  p.entitlement_kind='unlimited' AND p.is_free=0 AS unlimited,
  p.daily_message_limit AS raw_daily, p.monthly_message_limit AS raw_monthly, p.entitlement_kind,
  CASE WHEN initial.usable THEN initial.source ELSE '' END AS source,
  CASE WHEN initial.status IN ('active','trial') AND initial.expires_at<clock.now THEN 'expired'
       ELSE COALESCE(initial.status, '') END AS status,
  initial.expires_at, COALESCE(initial.usable, 0) AS has_subscription
 FROM admin_initial initial CROSS JOIN admin_clock clock JOIN plans p ON p.id=initial.plan_id
 UNION ALL
 SELECT a.user_id, 'apple:' || a.original_transaction_id, 1,
  ROW_NUMBER() OVER (PARTITION BY a.user_id ORDER BY CASE p.entitlement_kind WHEN 'unlimited' THEN 0 ELSE 1 END,
                   p.daily_message_limit DESC, a.expires_at DESC, a.original_transaction_id),
  p.id, p.code, p.name_en, p.is_free, p.entitlement_kind='unlimited' AND p.is_free=0,
  p.daily_message_limit, p.monthly_message_limit, p.entitlement_kind, 'app_store', 'active', a.expires_at, 1
 FROM apple_subscriptions a JOIN plans p ON p.id=a.plan_id CROSS JOIN admin_clock clock
 WHERE a.revoked_at IS NULL AND a.expires_at>clock.now
 UNION ALL
 SELECT g.user_id, 'google:' || g.token_hash, 2,
  ROW_NUMBER() OVER (PARTITION BY g.user_id ORDER BY p.entitlement_kind='unlimited' DESC,
                   p.daily_message_limit DESC, g.expires_at DESC, g.token_hash),
  p.id, p.code, p.name_en, p.is_free, p.entitlement_kind='unlimited' AND p.is_free=0,
  p.daily_message_limit, p.monthly_message_limit, p.entitlement_kind, 'google_play', 'active', g.expires_at, 1
 FROM google_subscriptions g JOIN plans p ON p.id=g.plan_id CROSS JOIN admin_clock clock
 WHERE g.revoked_at IS NULL AND g.superseded_by=''
  AND g.state IN ('SUBSCRIPTION_STATE_ACTIVE','SUBSCRIPTION_STATE_IN_GRACE_PERIOD','SUBSCRIPTION_STATE_CANCELED')
  AND (g.expires_at>clock.now OR (g.auto_renew=1 AND g.state<>'SUBSCRIPTION_STATE_CANCELED'
       AND g.read_at<g.expires_at AND g.expires_at>clock.bridge_cutoff))
 UNION ALL
 SELECT b.user_id, 'bank:' || period.order_id, 3,
  ROW_NUMBER() OVER (PARTITION BY b.user_id ORDER BY p.entitlement_kind='unlimited' DESC,
                   p.daily_message_limit DESC, period.expires_at DESC, period.order_id),
  p.id, p.code, p.name_en, p.is_free,
  COALESCE(json_extract(b.snapshot_json, '$.unlimited'), 0)=1 AND p.is_free=0,
  COALESCE(json_extract(b.snapshot_json, '$.daily_limit'), 0),
  COALESCE(json_extract(b.snapshot_json, '$.monthly_limit'), 0),
  CASE WHEN COALESCE(json_extract(b.snapshot_json, '$.unlimited'), 0)=1 THEN 'unlimited'
       WHEN COALESCE(json_extract(b.snapshot_json, '$.monthly_limit'), 0)>0 THEN 'monthly' ELSE 'daily' END,
  'bcc', 'active', period.expires_at, 1
 FROM bcc_subscription_periods period JOIN bcc_subscriptions b ON b.id=period.subscription_id
 JOIN plans p ON p.id=b.plan_id CROSS JOIN admin_clock clock
 WHERE period.revoked_at IS NULL AND period.starts_at<=clock.now AND period.expires_at>clock.now
),
admin_winners(user_id, step, candidate_id) AS (
 SELECT user_id, 0, candidate_id FROM admin_candidates WHERE provider_order=0
 UNION ALL
 SELECT winner.user_id, winner.step+1,
  CASE WHEN candidate.candidate_id IS NOT NULL AND (chosen.is_free=1 OR candidate.unlimited=1
       OR (chosen.unlimited=0 AND candidate.raw_daily>=chosen.raw_daily))
       THEN candidate.candidate_id ELSE winner.candidate_id END
 FROM admin_winners winner JOIN admin_candidates chosen ON chosen.candidate_id=winner.candidate_id
 LEFT JOIN admin_candidates candidate ON candidate.user_id=winner.user_id
  AND candidate.provider_order=winner.step+1 AND candidate.position=1
 WHERE winner.step<3
),
admin_effective AS (
 SELECT candidate.user_id, candidate.plan_id, candidate.code, candidate.name_en, candidate.is_free,
  candidate.unlimited, candidate.source, candidate.status, candidate.expires_at,
  CASE WHEN candidate.source='app_store' AND candidate.code IN ('base_monthly','base_yearly') THEN 0
       WHEN candidate.source IN ('bcc','google_play') AND candidate.entitlement_kind='monthly' THEN 0
       WHEN candidate.has_subscription=1 AND candidate.code='base_monthly' AND candidate.entitlement_kind='monthly'
        THEN COALESCE((SELECT CAST(value AS INTEGER) FROM system_settings WHERE key='apple_base_legacy_daily_units'), 0)
       ELSE candidate.raw_daily END AS daily_limit,
  CASE WHEN candidate.source='app_store' AND candidate.code IN ('base_monthly','base_yearly') THEN 0
       WHEN candidate.source IN ('bcc','google_play') AND candidate.entitlement_kind='monthly' THEN 0
       WHEN candidate.has_subscription=1 AND candidate.code='base_monthly' AND candidate.entitlement_kind='monthly' THEN 0
       ELSE candidate.raw_monthly END AS monthly_limit
 FROM admin_winners winner JOIN admin_candidates candidate ON candidate.candidate_id=winner.candidate_id
 WHERE winner.step=3
)
`

func adminEntitlementArgs(now time.Time) []any {
	return []any{ms(now), ms(now.Add(-googleRenewalBridge))}
}

func adminReportingTime(at []time.Time) time.Time {
	if len(at) > 0 {
		return at[0]
	}
	return time.Now().UTC()
}
