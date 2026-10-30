ALTER TABLE iap_products ADD COLUMN price_minor INTEGER NOT NULL DEFAULT 0;
ALTER TABLE iap_products ADD COLUMN currency TEXT NOT NULL DEFAULT 'KZT';
UPDATE iap_products SET price_minor=CASE code WHEN 'base_monthly' THEN 59900 WHEN 'base_yearly' THEN 599900 WHEN 'pro_monthly' THEN 299900 WHEN 'pro_yearly' THEN 2999000 WHEN 'pack30' THEN 10000 ELSE price_minor END WHERE code IN ('base_monthly','base_yearly','pro_monthly','pro_yearly','pack30');
-- Versioned business prices. Drafts never affect checkout; published snapshots
-- remain available for audit and pending purchases keep their original version.
CREATE TABLE billing_catalog_versions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    state TEXT NOT NULL CHECK(state IN ('draft','published','superseded')),
    base_version INTEGER NOT NULL DEFAULT 0,
    revision INTEGER NOT NULL DEFAULT 1,
    products_json TEXT NOT NULL,
    created_by TEXT NOT NULL,
    updated_by TEXT NOT NULL,
    published_by TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    published_at INTEGER
);
CREATE UNIQUE INDEX billing_catalog_one_draft ON billing_catalog_versions(state) WHERE state='draft';
CREATE UNIQUE INDEX billing_catalog_one_published ON billing_catalog_versions(state) WHERE state='published';
CREATE TABLE billing_catalog_audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    version_id INTEGER NOT NULL REFERENCES billing_catalog_versions(id),
    action TEXT NOT NULL,
    actor TEXT NOT NULL,
    before_json TEXT NOT NULL,
    after_json TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE TABLE billing_orders (
    id TEXT PRIMARY KEY,
    user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    idempotency_key TEXT NOT NULL,
    catalog_version INTEGER NOT NULL REFERENCES billing_catalog_versions(id),
    product_code TEXT NOT NULL,
    snapshot_json TEXT NOT NULL,
    amount_minor INTEGER NOT NULL CHECK(amount_minor>0),
    currency TEXT NOT NULL CHECK(currency='KZT'),
    provider TEXT NOT NULL,
    environment TEXT NOT NULL CHECK(environment IN ('sandbox','production')),
    bank_order TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK(status IN ('created','pending','verification_pending','needs_review','succeeded','failed','canceled','refunded')),
    checkout_token_hash TEXT NOT NULL,
    request_json TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(user_id,idempotency_key)
);
CREATE TABLE billing_order_events (
    id TEXT PRIMARY KEY,
    order_id TEXT NOT NULL REFERENCES billing_orders(id),
    source TEXT NOT NULL,
    state TEXT NOT NULL,
    fields_json TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX billing_orders_pending ON billing_orders(environment,status,created_at);
ALTER TABLE credit_grants ADD COLUMN billing_order_id TEXT REFERENCES billing_orders(id);
CREATE UNIQUE INDEX credit_grants_billing_order ON credit_grants(billing_order_id) WHERE billing_order_id IS NOT NULL;
CREATE TABLE billing_bank_operations (
 id TEXT PRIMARY KEY,
 order_id TEXT NOT NULL REFERENCES billing_orders(id),
 kind TEXT NOT NULL CHECK(kind='refund'),
 status TEXT NOT NULL CHECK(status IN ('pending','succeeded','needs_review')),
 request_json TEXT NOT NULL,
 created_by TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 UNIQUE(order_id,kind)
);
