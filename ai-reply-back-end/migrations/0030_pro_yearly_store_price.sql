-- The owner selected 34,900 KZT/year, an available App Store price point.
-- Price only: retain store identities, unlimited access, sold order/mandate
-- snapshots, signed Apple amounts, credit balances and all prior publications.
UPDATE plans SET price = 3490000, updated_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE code = 'pro_yearly' AND price <> 3490000;
UPDATE iap_products SET price_minor = 3490000, updated_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE code = 'pro_yearly' AND price_minor <> 3490000;

-- A catalog already aligned through the admin API needs no extra publication.
-- Fresh databases have no publication yet; EnsureInitial uses the new default.
INSERT INTO billing_catalog_versions
 (state,base_version,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at)
SELECT 'superseded', id,
 (SELECT json_group_array(json(CASE json_extract(value, '$.code')
  WHEN 'pro_yearly' THEN json_set(value, '$.price_minor', 3490000)
  ELSE value END)) FROM json_each(products_json)),
 'migration:0030', 'migration:0030', 'migration:0030',
 CAST(strftime('%s', 'now') AS INTEGER) * 1000,
 CAST(strftime('%s', 'now') AS INTEGER) * 1000,
 CAST(strftime('%s', 'now') AS INTEGER) * 1000
FROM billing_catalog_versions
WHERE state = 'published' AND EXISTS (
 SELECT 1 FROM json_each(products_json)
 WHERE json_extract(value, '$.code') = 'pro_yearly' AND json_extract(value, '$.price_minor') <> 3490000
);

-- Preserve the owner's draft names, flags and other edits. Align this one price
-- and rebase a draft of the replaced publication; an already stale draft stays
-- stale. Bump revision so a browser holding the old draft cannot overwrite it.
INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at)
SELECT id, 'rebase_price', 'migration:0030', products_json,
 (SELECT json_group_array(json(CASE json_extract(value, '$.code')
  WHEN 'pro_yearly' THEN json_set(value, '$.price_minor', 3490000)
  ELSE value END)) FROM json_each(products_json)),
 CAST(strftime('%s', 'now') AS INTEGER) * 1000
FROM billing_catalog_versions
WHERE state = 'draft' AND (
 EXISTS (SELECT 1 FROM json_each(products_json)
  WHERE json_extract(value, '$.code') = 'pro_yearly' AND json_extract(value, '$.price_minor') <> 3490000)
 OR base_version = (SELECT base_version FROM billing_catalog_versions WHERE created_by = 'migration:0030')
);
UPDATE billing_catalog_versions SET
 products_json = (SELECT json_group_array(json(CASE json_extract(value, '$.code')
  WHEN 'pro_yearly' THEN json_set(value, '$.price_minor', 3490000)
  ELSE value END)) FROM json_each(products_json)),
 base_version = CASE
  WHEN base_version = (SELECT base_version FROM billing_catalog_versions WHERE created_by = 'migration:0030')
  THEN (SELECT id FROM billing_catalog_versions WHERE created_by = 'migration:0030')
  ELSE base_version END,
 revision = revision + 1, updated_by = 'migration:0030',
 updated_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE state = 'draft' AND (
 EXISTS (SELECT 1 FROM json_each(products_json)
  WHERE json_extract(value, '$.code') = 'pro_yearly' AND json_extract(value, '$.price_minor') <> 3490000)
 OR base_version = (SELECT base_version FROM billing_catalog_versions WHERE created_by = 'migration:0030')
);

UPDATE billing_catalog_versions SET state = 'superseded'
WHERE state = 'published'
 AND EXISTS (SELECT 1 FROM billing_catalog_versions WHERE created_by = 'migration:0030');
UPDATE billing_catalog_versions SET state = 'published' WHERE created_by = 'migration:0030';
INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at)
SELECT current.id, 'publish', 'migration:0030', previous.products_json, current.products_json, current.published_at
FROM billing_catalog_versions current JOIN billing_catalog_versions previous ON previous.id = current.base_version
WHERE current.created_by = 'migration:0030';
