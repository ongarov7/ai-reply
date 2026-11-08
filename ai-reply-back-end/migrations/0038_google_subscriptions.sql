-- Google Play auto-renewing subscriptions, off until an administrator maps
-- Google ids and enables Android for them.
--
-- google_subscriptions mirrors apple_subscriptions: one row per purchase token,
-- keyed by its SHA-256 hex. The raw token is kept only AES-GCM sealed, because
-- the worker has to read the subscription again near expiry. iap_purchases
-- keeps the financial record, one row per paid order of the token.

-- Each store can now be switched off for a product on its own.
ALTER TABLE iap_products ADD COLUMN off_ios INTEGER NOT NULL DEFAULT 0;
ALTER TABLE iap_products ADD COLUMN off_android INTEGER NOT NULL DEFAULT 0;

CREATE TABLE google_subscriptions (
 token_hash TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 purchase_token_sealed TEXT,
 product_id TEXT NOT NULL,
 product_code TEXT NOT NULL DEFAULT '',
 plan_id TEXT NOT NULL REFERENCES plans(id),
 environment TEXT NOT NULL,
 state TEXT NOT NULL,
 order_id TEXT NOT NULL DEFAULT '',
 started_at INTEGER NOT NULL,
 expires_at INTEGER NOT NULL,
 auto_renew INTEGER NOT NULL DEFAULT 1,
 linked_token_hash TEXT NOT NULL DEFAULT '',
 superseded_by TEXT NOT NULL DEFAULT '',
 granted_at INTEGER NOT NULL,
 acknowledged_at INTEGER,
 ack_abandoned_at INTEGER,
 revoked_at INTEGER,
 revoke_reason TEXT NOT NULL DEFAULT '',
 revoked_order_id TEXT NOT NULL DEFAULT '',
 read_at INTEGER NOT NULL,
 next_check_at INTEGER,
 check_failures INTEGER NOT NULL DEFAULT 0,
 updated_at INTEGER NOT NULL
);
CREATE INDEX idx_google_subscriptions_user ON google_subscriptions(user_id, expires_at);
CREATE INDEX idx_google_subscriptions_due ON google_subscriptions(next_check_at) WHERE next_check_at IS NOT NULL;
CREATE INDEX idx_google_subscriptions_linked ON google_subscriptions(linked_token_hash) WHERE linked_token_hash <> '';
CREATE INDEX idx_google_subscriptions_unacknowledged ON google_subscriptions(granted_at)
 WHERE acknowledged_at IS NULL AND ack_abandoned_at IS NULL;

-- A managed product already mapped to Google outside the catalog stays sold as
-- it is today once the catalog learns about its id. No such product: no change.
CREATE TEMP TABLE google_ids_0038 AS
SELECT code, google_product_id, is_active FROM iap_products
WHERE archived_at IS NULL AND coalesce(google_product_id, '') <> ''
 AND code IN ('base_monthly', 'base_yearly', 'pro_monthly', 'pro_yearly', 'pack30');

CREATE TEMP TABLE catalog_0038 AS
SELECT v.id, v.state, v.base_version, v.products_json AS before_json,
 (SELECT json_group_array(json(CASE WHEN g.code IS NULL THEN j.value
    ELSE json_set(j.value, '$.google_product_id', g.google_product_id, '$.available_android', json(CASE WHEN g.is_active = 1 THEN 'true' ELSE 'false' END)) END))
  FROM json_each(v.products_json) j LEFT JOIN google_ids_0038 g ON g.code = json_extract(j.value, '$.code')) AS after_json,
 EXISTS (SELECT 1 FROM json_each(v.products_json) j JOIN google_ids_0038 g ON g.code = json_extract(j.value, '$.code')
  WHERE coalesce(json_extract(j.value, '$.google_product_id'), '') <> g.google_product_id) AS changed
FROM billing_catalog_versions v WHERE v.state IN ('published', 'draft');

INSERT INTO billing_catalog_versions
 (state,base_version,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at)
SELECT 'superseded', id, after_json, 'migration:0038', 'migration:0038', 'migration:0038',
 CAST(strftime('%s', 'now') AS INTEGER) * 1000,
 CAST(strftime('%s', 'now') AS INTEGER) * 1000,
 CAST(strftime('%s', 'now') AS INTEGER) * 1000
FROM catalog_0038 WHERE state = 'published' AND changed;

INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at)
SELECT c.id, 'rebase_store_ids', 'migration:0038', c.before_json, c.after_json,
 CAST(strftime('%s', 'now') AS INTEGER) * 1000
FROM catalog_0038 c
WHERE c.state = 'draft' AND (c.changed
 OR c.base_version = (SELECT base_version FROM billing_catalog_versions WHERE created_by = 'migration:0038'));
UPDATE billing_catalog_versions SET
 products_json = (SELECT c.after_json FROM catalog_0038 c WHERE c.id = billing_catalog_versions.id),
 base_version = CASE
  WHEN base_version = (SELECT base_version FROM billing_catalog_versions WHERE created_by = 'migration:0038')
  THEN (SELECT id FROM billing_catalog_versions WHERE created_by = 'migration:0038')
  ELSE base_version END,
 revision = revision + 1, updated_by = 'migration:0038',
 updated_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE state = 'draft' AND EXISTS (SELECT 1 FROM catalog_0038 c WHERE c.id = billing_catalog_versions.id AND (c.changed
 OR c.base_version = (SELECT base_version FROM billing_catalog_versions WHERE created_by = 'migration:0038')));

UPDATE billing_catalog_versions SET state = 'superseded'
WHERE state = 'published'
 AND EXISTS (SELECT 1 FROM billing_catalog_versions WHERE created_by = 'migration:0038');
UPDATE billing_catalog_versions SET state = 'published' WHERE created_by = 'migration:0038';
INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at)
SELECT current.id, 'publish', 'migration:0038', previous.products_json, current.products_json, current.published_at
FROM billing_catalog_versions current JOIN billing_catalog_versions previous ON previous.id = current.base_version
WHERE current.created_by = 'migration:0038';

DROP TABLE catalog_0038;
DROP TABLE google_ids_0038;
