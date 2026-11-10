-- Apple-only lineup change. Keep Base product mappings, plan terms, renewals,
-- grants and order snapshots intact for existing and delayed transactions.
-- Android and web availability are deliberately unchanged.
-- An administrator may already have created this exact product through the
-- existing API. Adopt its UUID, grants and transaction history only when the
-- durable identity and sold terms match. A conflicting code or store ID must
-- abort the complete migration instead of silently changing a paid contract.
CREATE TEMP TABLE pack500_identity_0039 (
 valid INTEGER NOT NULL CONSTRAINT canonical_pack500_required CHECK(valid=1)
);
INSERT INTO pack500_identity_0039
SELECT NOT EXISTS(SELECT 1 FROM iap_products
 WHERE (code='pack500' OR apple_product_id='kz.aireply.pack500') AND NOT COALESCE((
  code='pack500' AND apple_product_id='kz.aireply.pack500' AND kind='pack'
  AND units=500 AND duration_hours=0 AND plan_id='' AND billing_period=''
  AND price_minor=99000 AND currency='KZT'
  AND COALESCE(google_product_id,'')='' AND is_active=1
  AND off_ios=0 AND archived_at IS NULL
 ),0));
INSERT INTO pack500_identity_0039
SELECT NOT EXISTS(SELECT 1 FROM billing_catalog_versions,json_each(products_json)
 WHERE state IN ('published','draft')
 AND (json_extract(value,'$.code')='pack500' OR json_extract(value,'$.apple_product_id')='kz.aireply.pack500')
 AND NOT COALESCE(
  json_extract(value,'$.code')='pack500' AND json_extract(value,'$.kind')='pack'
  AND json_extract(value,'$.units')=500 AND json_extract(value,'$.price_minor')=99000
  AND json_extract(value,'$.currency')='KZT' AND json_extract(value,'$.billing_period')=''
  AND json_extract(value,'$.daily_limit')=0 AND COALESCE(json_extract(value,'$.monthly_limit'),0)=0
  AND json_extract(value,'$.unlimited')=0 AND json_extract(value,'$.apple_product_id')='kz.aireply.pack500'
  AND COALESCE(json_extract(value,'$.google_product_id'),'')=''
  AND json_extract(value,'$.available_ios')=1 AND json_extract(value,'$.available_web')=0
  AND COALESCE(json_extract(value,'$.available_android'),0)=0 AND json_extract(value,'$.active')=1,0));
DROP TABLE pack500_identity_0039;

UPDATE iap_products SET off_ios=1,
 updated_at=CAST(strftime('%s','now') AS INTEGER)*1000
WHERE code IN ('base_monthly','base_yearly');

-- A consumable needs its own durable identity; never reinterpret pack30 or a
-- subscription as the new 500-reply contract. Pack grants already have no expiry.
INSERT INTO iap_products
 (id,code,kind,units,duration_hours,apple_product_id,name_kk,name_ru,name_en,name_uz,
 is_active,sort_order,created_at,updated_at,price_minor,currency,off_ios,off_android)
SELECT
 '00000000-0000-4000-8000-000000000206','pack500','pack',500,0,'kz.aireply.pack500',
 '500 жауап','500 ответов','500 replies','500 javob',1,50,
 CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000,
 99000,'KZT',0,1
WHERE NOT EXISTS(SELECT 1 FROM iap_products WHERE code='pack500');
-- Older admin APIs had no per-store switch input. With no Google ID they
-- already offered this product only on Apple; make that intent explicit.
UPDATE iap_products SET off_android=1,
 updated_at=CAST(strftime('%s','now') AS INTEGER)*1000
WHERE code='pack500' AND off_android<>1;

CREATE TEMP TABLE catalog_0039 AS
SELECT id,state,base_version,products_json AS before_json,
 (SELECT json_group_array(json(CASE
  WHEN json_extract(value,'$.code') IN ('base_monthly','base_yearly')
  THEN json_set(value,'$.available_ios',json('false')) ELSE value END))
  FROM json_each(products_json)) AS after_json,
 EXISTS(SELECT 1 FROM json_each(products_json)
  WHERE json_extract(value,'$.code') IN ('base_monthly','base_yearly')
   AND json_extract(value,'$.available_ios')<>0)
 OR NOT EXISTS(SELECT 1 FROM json_each(products_json)
  WHERE json_extract(value,'$.code')='pack500') AS changed
FROM billing_catalog_versions WHERE state IN ('published','draft');

UPDATE catalog_0039 SET after_json=json_insert(after_json,'$[#]',json(
 '{"code":"pack500","kind":"pack","name":{"ru":"500 ответов","en":"500 replies","kk":"500 жауап","uz":"500 javob"},"price_minor":99000,"currency":"KZT","billing_period":"","units":500,"daily_limit":0,"unlimited":false,"apple_product_id":"kz.aireply.pack500","available_ios":true,"available_web":false,"active":true}'
)) WHERE NOT EXISTS(SELECT 1 FROM json_each(after_json)
 WHERE json_extract(value,'$.code')='pack500');

INSERT INTO billing_catalog_versions
 (state,base_version,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at)
SELECT 'superseded',id,after_json,'migration:0039','migration:0039','migration:0039',
 CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000,
 CAST(strftime('%s','now') AS INTEGER)*1000
FROM catalog_0039 WHERE state='published' AND changed;

-- Rebase a current draft without discarding the owner's unrelated edits. A
-- stale draft remains stale and cannot overwrite a newer publication.
INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at)
SELECT id,'rebase_apple_pack','migration:0039',before_json,after_json,
 CAST(strftime('%s','now') AS INTEGER)*1000
FROM catalog_0039 WHERE state='draft' AND (changed OR base_version=
 (SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0039'));
UPDATE billing_catalog_versions SET
 products_json=(SELECT after_json FROM catalog_0039 WHERE id=billing_catalog_versions.id),
 base_version=CASE WHEN base_version=(SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0039')
  THEN (SELECT id FROM billing_catalog_versions WHERE created_by='migration:0039') ELSE base_version END,
 revision=revision+1,updated_by='migration:0039',
 updated_at=CAST(strftime('%s','now') AS INTEGER)*1000
WHERE state='draft' AND id IN (SELECT id FROM catalog_0039 WHERE changed OR base_version=
 (SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0039'));

UPDATE billing_catalog_versions SET state='superseded'
WHERE state='published' AND id=(SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0039');
UPDATE billing_catalog_versions SET state='published' WHERE created_by='migration:0039';
INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at)
SELECT current.id,'publish','migration:0039',previous.products_json,current.products_json,current.published_at
FROM billing_catalog_versions current JOIN billing_catalog_versions previous ON previous.id=current.base_version
WHERE current.created_by='migration:0039';
DROP TABLE catalog_0039;
