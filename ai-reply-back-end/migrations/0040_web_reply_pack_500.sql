-- New web/Mac purchases use the same one-time 500-reply offer as iOS.
-- Retire Base only from new web checkout. Its durable store identities, legacy
-- plan allowance, paid grants, order snapshots and recurring mandates remain
-- unchanged. Payment providers still fulfill their own authenticated orders.
CREATE TEMP TABLE catalog_0040 AS
SELECT id,state,base_version,products_json AS before_json,
 (SELECT json_group_array(json(CASE json_extract(value,'$.code')
  WHEN 'base_monthly' THEN json_set(value,'$.available_web',json('false'))
  WHEN 'pack500' THEN json_set(value,'$.available_web',json('true'))
  ELSE value END)) FROM json_each(products_json)) AS after_json,
 EXISTS(SELECT 1 FROM json_each(products_json) WHERE
  (json_extract(value,'$.code')='base_monthly' AND json_extract(value,'$.available_web')<>0)
  OR (json_extract(value,'$.code')='pack500' AND json_extract(value,'$.available_web')<>1)) AS changed
FROM billing_catalog_versions WHERE state IN ('published','draft');

INSERT INTO billing_catalog_versions
 (state,base_version,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at)
SELECT 'superseded',id,after_json,'migration:0040','migration:0040','migration:0040',
 CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000,
 CAST(strftime('%s','now') AS INTEGER)*1000
FROM catalog_0040 WHERE state='published' AND changed;

-- Keep unrelated draft edits and preserve the stale-draft conflict guard.
INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at)
SELECT id,'rebase_web_pack','migration:0040',before_json,after_json,
 CAST(strftime('%s','now') AS INTEGER)*1000
FROM catalog_0040 WHERE state='draft' AND (changed OR base_version=
 (SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0040'));
UPDATE billing_catalog_versions SET
 products_json=(SELECT after_json FROM catalog_0040 WHERE id=billing_catalog_versions.id),
 base_version=CASE WHEN base_version=(SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0040')
  THEN (SELECT id FROM billing_catalog_versions WHERE created_by='migration:0040') ELSE base_version END,
 revision=revision+1,updated_by='migration:0040',
 updated_at=CAST(strftime('%s','now') AS INTEGER)*1000
WHERE state='draft' AND id IN (SELECT id FROM catalog_0040 WHERE changed OR base_version=
 (SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0040'));

UPDATE billing_catalog_versions SET state='superseded'
WHERE state='published' AND id=(SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0040');
UPDATE billing_catalog_versions SET state='published' WHERE created_by='migration:0040';
INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at)
SELECT current.id,'publish','migration:0040',previous.products_json,current.products_json,current.published_at
FROM billing_catalog_versions current JOIN billing_catalog_versions previous ON previous.id=current.base_version
WHERE current.created_by='migration:0040';
DROP TABLE catalog_0040;
