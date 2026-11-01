-- Bank mandates and delivered periods are independent of Apple/Google purchases.
CREATE TABLE bcc_subscriptions (
 id TEXT PRIMARY KEY,
 user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
 environment TEXT NOT NULL CHECK(environment IN ('sandbox','production')),
 root_order_id TEXT NOT NULL UNIQUE REFERENCES billing_orders(id),
 plan_id TEXT NOT NULL REFERENCES plans(id),
 snapshot_json TEXT NOT NULL,
 amount_minor INTEGER NOT NULL CHECK(amount_minor>0),
 period_days INTEGER NOT NULL CHECK(period_days IN (30,365)),
 status TEXT NOT NULL CHECK(status IN ('pending','active','canceled','expired','needs_review')),
 mandate_expires_at INTEGER NOT NULL,
 first_rrn TEXT NOT NULL DEFAULT '',
 first_int_ref TEXT NOT NULL DEFAULT '',
 merch_rn_id TEXT NOT NULL,
 paid_until INTEGER,
 next_due_at INTEGER,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX bcc_one_renewing_subscription ON bcc_subscriptions(user_id,environment)
 WHERE status IN ('pending','active','needs_review') AND user_id IS NOT NULL;
CREATE INDEX bcc_subscriptions_due ON bcc_subscriptions(status,next_due_at);
CREATE TABLE bcc_subscription_periods (
 order_id TEXT PRIMARY KEY REFERENCES billing_orders(id),
 subscription_id TEXT NOT NULL REFERENCES bcc_subscriptions(id),
 starts_at INTEGER NOT NULL,
 expires_at INTEGER NOT NULL,
 revoked_at INTEGER
);
CREATE TABLE bcc_recurring_attempts (
 order_id TEXT PRIMARY KEY REFERENCES billing_orders(id),
 subscription_id TEXT NOT NULL REFERENCES bcc_subscriptions(id),
 period_start INTEGER NOT NULL,
 submitted_at INTEGER NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('submitted','uncertain','confirmed','declined')),
 UNIQUE(subscription_id,period_start)
);
ALTER TABLE billing_orders ADD COLUMN bcc_subscription_id TEXT REFERENCES bcc_subscriptions(id);
CREATE TABLE billing_order_context (
 order_id TEXT PRIMARY KEY REFERENCES billing_orders(id),
 locale TEXT NOT NULL,
 recurring_terms_version TEXT NOT NULL DEFAULT '',
 recurring_until TEXT NOT NULL DEFAULT ''
);
CREATE TABLE billing_receipt_outbox (
 order_id TEXT PRIMARY KEY REFERENCES billing_orders(id),
 user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
 locale TEXT NOT NULL,
 params_json TEXT NOT NULL,
 kind TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sending','sent','needs_review')),
 attempts INTEGER NOT NULL DEFAULT 0,
 created_at INTEGER NOT NULL,
 next_attempt_at INTEGER NOT NULL,
 first_attempt_at INTEGER,
 delivery_json TEXT NOT NULL DEFAULT '',
 claim_token TEXT NOT NULL DEFAULT '',
 lease_until INTEGER NOT NULL DEFAULT 0,
 sent_at INTEGER
);
CREATE INDEX billing_receipt_due ON billing_receipt_outbox(status,next_attempt_at,lease_until);
