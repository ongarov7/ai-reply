-- 0015_credits: сәлемдесу бонусы, кредит журналы және генерация брондары.
--
-- A generation is now served from one of several buckets, in this order: the
-- plan's daily allowance, an active pass, the welcome bonus, admin grants,
-- purchased packs (oldest first). Everything that is not the plan lives in
-- credit_grants; every movement of a grant (grant, consume, refund, revoke)
-- is a row in credit_ledger, so a balance can always be explained.
--
-- usage_daily.extra_used / usage_monthly.extra_used count generations served
-- from the extra buckets. They never touch `used`, so a monthly plan cap is
-- not eaten by credits and never blocks them.
--
-- usage_reservations holds one row per charged generation: which bucket paid
-- for it and whether it is still running (reserved), delivered (committed) or
-- given back (refunded). It carries the optional Idempotency-Key of the
-- request (one charge per key) and lets a sweep refund what a crashed process
-- left reserved. No text of any kind is stored here.
--
-- The welcome bonus is once per account: the partial UNIQUE index on
-- credit_grants (user_id) WHERE kind = 'welcome'. Accounts are hard-deleted, so
-- deleting an account and signing up again gets a new bonus; nothing about a
-- deleted account is kept (privacy), which is an accepted limitation.
-- welcome_bonus_since is the moment this migration ran: accounts created
-- before it never get the bonus. welcome_bonus_units is the admin setting
-- (0 = off).
--
-- credit_grants.iap_purchase_id stays a plain column: 0018_iap adds the
-- purchases table it points to.
--
-- Rows go away with the account (ON DELETE CASCADE).
--
-- Only additive. Rollback: older binaries ignore the new tables and columns
-- and charge the plan only (credits stay untouched until the new binary is
-- back). The tables can be dropped by hand afterwards.
ALTER TABLE usage_daily ADD COLUMN extra_used INTEGER NOT NULL DEFAULT 0;
ALTER TABLE usage_monthly ADD COLUMN extra_used INTEGER NOT NULL DEFAULT 0;

CREATE TABLE credit_grants (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind            TEXT NOT NULL,                      -- welcome | admin | pack | pass
    units_total     INTEGER NOT NULL CHECK (units_total >= 0),
    units_remaining INTEGER NOT NULL CHECK (units_remaining >= 0),
    starts_at       INTEGER NOT NULL,
    expires_at      INTEGER,                            -- NULL = never (welcome, admin, pack)
    status          TEXT NOT NULL DEFAULT 'active',     -- active | revoked
    iap_purchase_id TEXT,                               -- the store purchase behind a pack or a pass (0018_iap)
    product_code    TEXT NOT NULL DEFAULT '',
    granted_by      TEXT NOT NULL DEFAULT 'system',     -- system | admin_users.id
    reason          TEXT NOT NULL DEFAULT '',           -- the administrator's note on a manual grant
    revoked_at      INTEGER,
    revoked_by      TEXT NOT NULL DEFAULT '',
    revoke_reason   TEXT NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    CHECK (units_remaining <= units_total)
);
CREATE UNIQUE INDEX idx_credit_grants_welcome ON credit_grants (user_id) WHERE kind = 'welcome';
CREATE UNIQUE INDEX idx_credit_grants_purchase ON credit_grants (iap_purchase_id) WHERE iap_purchase_id IS NOT NULL;
CREATE INDEX idx_credit_grants_user ON credit_grants (user_id, status, kind);

CREATE TABLE credit_ledger (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    grant_id       TEXT REFERENCES credit_grants (id) ON DELETE SET NULL,
    reservation_id TEXT,                                -- usage_reservations.id for consume | refund
    entry          TEXT NOT NULL,                       -- grant | consume | refund | revoke
    kind           TEXT NOT NULL,                       -- the grant's kind: welcome | admin | pack | pass
    delta          INTEGER NOT NULL,
    actor          TEXT NOT NULL DEFAULT 'system',      -- system | admin_users.id
    reason         TEXT NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_credit_ledger_reservation ON credit_ledger (reservation_id, entry) WHERE reservation_id IS NOT NULL;
CREATE INDEX idx_credit_ledger_user ON credit_ledger (user_id, created_at);

CREATE TABLE usage_reservations (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    idempotency_key TEXT,                               -- the request's Idempotency-Key, NULL without one
    mode            TEXT NOT NULL,                      -- reply | compose
    bucket          TEXT NOT NULL,                      -- daily | pass | welcome | admin | pack
    grant_id        TEXT REFERENCES credit_grants (id) ON DELETE SET NULL,
    usage_date      TEXT NOT NULL,                      -- usage_daily key of the charge
    usage_month     TEXT NOT NULL,                      -- usage_monthly key of the charge
    status          TEXT NOT NULL,                      -- reserved | committed | refunded
    replays         INTEGER NOT NULL DEFAULT 0,         -- free regenerations served for the key
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_usage_reservations_key ON usage_reservations (user_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
CREATE INDEX idx_usage_reservations_status ON usage_reservations (status, created_at);
CREATE INDEX idx_usage_reservations_user ON usage_reservations (user_id);

INSERT OR IGNORE INTO system_settings (key, value, updated_at) VALUES ('welcome_bonus_units', '20', 0);
INSERT OR IGNORE INTO system_settings (key, value, updated_at)
VALUES ('welcome_bonus_since', CAST(CAST(strftime('%s', 'now') AS INTEGER) * 1000 AS TEXT), 0);
