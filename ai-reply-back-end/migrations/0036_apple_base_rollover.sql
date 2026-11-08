-- Retain exact signed Apple periods; apple_subscriptions keeps only the latest
-- renewal. Older periods are recovered when their signed transactions are replayed.
CREATE TABLE apple_base_credit_periods (
 purchase_id TEXT PRIMARY KEY REFERENCES iap_purchases(id) ON DELETE CASCADE,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 original_transaction_id TEXT NOT NULL,
 starts_at INTEGER NOT NULL,
 expires_at INTEGER NOT NULL,
 accrual_cap_at INTEGER,
 upgraded INTEGER NOT NULL DEFAULT 0,
 daily_units INTEGER NOT NULL CHECK(daily_units > 0),
 signed_at INTEGER NOT NULL,
 revoked_at INTEGER
);
CREATE INDEX apple_base_periods_user ON apple_base_credit_periods(user_id);

-- One paid allowance per account/calendar date, including renewal overlap.
-- The grant has no expiry and is spendable through the existing pack bucket.
CREATE TABLE apple_base_credit_days (
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 usage_date TEXT NOT NULL,
 starts_at INTEGER NOT NULL,
 ends_at INTEGER NOT NULL,
 purchase_id TEXT NOT NULL REFERENCES apple_base_credit_periods(purchase_id) ON DELETE CASCADE,
 grant_id TEXT NOT NULL UNIQUE REFERENCES credit_grants(id) ON DELETE CASCADE,
 PRIMARY KEY(user_id,usage_date)
);

-- A pre-rollover in-flight daily charge may later fail and must refund the
-- permanent grant from which its already-used unit was deducted.
CREATE TABLE apple_base_legacy_charges (
 reservation_id TEXT PRIMARY KEY REFERENCES usage_reservations(id) ON DELETE CASCADE,
 grant_id TEXT NOT NULL REFERENCES credit_grants(id) ON DELETE CASCADE
);

INSERT INTO apple_base_credit_periods
 (purchase_id,user_id,original_transaction_id,starts_at,expires_at,daily_units,signed_at,revoked_at)
SELECT purchase.id,a.user_id,a.original_transaction_id,a.purchased_at,a.expires_at,
 plan.daily_message_limit,a.signed_at,a.revoked_at
FROM apple_subscriptions a
JOIN iap_purchases purchase ON purchase.provider='app_store' AND purchase.transaction_id=a.transaction_id
JOIN plans plan ON plan.id=a.plan_id
WHERE plan.code IN ('base_monthly','base_yearly') AND plan.daily_message_limit>0;
