-- 0018_iap: App Store және Google Play арқылы сатып алынған жауап пакеттері мен pass.
--
-- iap_products is what an administrator configured: what a store product gives
-- (kind pack | pass, units, duration_hours for a pass) and its ids in App Store
-- Connect and Play Console. No product is seeded and no price is stored: the
-- apps show the store's localized price, and a product stays hidden until an
-- admin gives it units and a store id, makes it active and switches
-- iap_enabled on. Archived products keep their store ids (UNIQUE), so a
-- purchase already paid for still maps to what it bought.
--
-- iap_purchases holds one row per store transaction, UNIQUE (provider,
-- transaction_id): a re-posted transaction can never be granted twice, and one
-- bound to an account is never granted to another. transaction_id is Apple's
-- transactionId or the SHA-256 hex of the Google purchase token; the raw token
-- is not kept in the clear. purchase_token_sealed holds it AES-GCM sealed with
-- a server key only until the server has consumed the purchase (a failed
-- consume is retried, Google refunds an unconsumed purchase after 3 days), and
-- is cleared then. environment separates real money (Production) from Sandbox
-- (TestFlight, App Review) and Google test purchases.
--
-- The grant itself is a credit_grants row (kind pack | pass) whose
-- iap_purchase_id points here; refunds revoke what is left of it through the
-- ledger (0015_credits).
--
-- Purchases are financial records: when an account is deleted the row stays
-- with user_id NULL (ON DELETE SET NULL) and nothing that names the person;
-- the credit grant and the ledger go with the account (ON DELETE CASCADE).
--
-- iap_notifications makes store notifications idempotent: UNIQUE (provider,
-- notification_id), App Store notificationUUID. Only the type and the outcome
-- are stored, never the signed payload.
--
-- Only additive. Rollback: older binaries do not know the tables and never
-- sell anything (iap_enabled stays unread); drop them by hand afterwards
-- (iap_purchases after the credit grants that point to it are gone).
CREATE TABLE iap_products (
    id                TEXT PRIMARY KEY,
    code              TEXT NOT NULL UNIQUE,
    kind              TEXT NOT NULL,                      -- pack | pass
    units             INTEGER NOT NULL DEFAULT 0,         -- generations one purchase gives
    duration_hours    INTEGER NOT NULL DEFAULT 0,         -- pass only: how long the units last
    apple_product_id  TEXT UNIQUE,                        -- App Store Connect product id, NULL = not on iOS
    google_product_id TEXT UNIQUE,                        -- Play Console product id, NULL = not on Android
    name_kk           TEXT NOT NULL DEFAULT '',
    name_ru           TEXT NOT NULL DEFAULT '',
    name_en           TEXT NOT NULL DEFAULT '',
    name_uz           TEXT NOT NULL DEFAULT '',
    is_active         INTEGER NOT NULL DEFAULT 0,
    sort_order        INTEGER NOT NULL DEFAULT 0,
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    archived_at       INTEGER
);

CREATE TABLE iap_purchases (
    id                      TEXT PRIMARY KEY,
    user_id                 TEXT REFERENCES users (id) ON DELETE SET NULL,
    provider                TEXT NOT NULL,                -- app_store | google_play
    environment             TEXT NOT NULL DEFAULT '',     -- Production | Sandbox | Test | Promo
    store_product_id        TEXT NOT NULL DEFAULT '',
    product_code            TEXT NOT NULL DEFAULT '',
    transaction_id          TEXT NOT NULL,                -- Apple transactionId | sha256 hex of the Google token
    original_transaction_id TEXT NOT NULL DEFAULT '',
    order_id                TEXT NOT NULL DEFAULT '',     -- Google order id (GPA.…)
    quantity                INTEGER NOT NULL DEFAULT 1,
    units                   INTEGER NOT NULL DEFAULT 0,   -- what the grant gave (units × quantity)
    status                  TEXT NOT NULL,                -- pending | granted | refunded | revoked | rejected
    purchase_token_sealed   TEXT,                         -- Google only, until consumed
    consumed_at             INTEGER,                      -- Google: consumed by this server
    purchased_at            INTEGER,
    revoked_at              INTEGER,
    revoke_reason           TEXT NOT NULL DEFAULT '',
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_iap_purchases_txn ON iap_purchases (provider, transaction_id);
CREATE INDEX idx_iap_purchases_user ON iap_purchases (user_id, created_at);
CREATE INDEX idx_iap_purchases_created ON iap_purchases (created_at);
CREATE INDEX idx_iap_purchases_unconsumed ON iap_purchases (provider, created_at)
    WHERE status = 'granted' AND consumed_at IS NULL;

CREATE TABLE iap_notifications (
    id              TEXT PRIMARY KEY,
    provider        TEXT NOT NULL,
    notification_id TEXT NOT NULL,                        -- App Store notificationUUID
    type            TEXT NOT NULL,
    subtype         TEXT NOT NULL DEFAULT '',
    transaction_id  TEXT NOT NULL DEFAULT '',
    outcome         TEXT NOT NULL,                        -- revoked | restored | logged | ignored | unknown_transaction
    received_at     INTEGER NOT NULL,
    UNIQUE (provider, notification_id)
);

INSERT OR IGNORE INTO system_settings (key, value, updated_at) VALUES ('iap_enabled', 'false', 0);
