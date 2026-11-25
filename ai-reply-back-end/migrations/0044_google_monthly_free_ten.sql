-- Android uses Pro Monthly (unlimited, auto-renewing) at 990 KZT; the
-- platform-specific expected price is configured separately from iOS/web.
-- Retire only the old Android 500-reply offer. Never delete its identity or
-- touch existing purchases, grants, subscription contracts or receipt money.
UPDATE iap_products SET off_android=1,
 updated_at=CAST(strftime('%s','now') AS INTEGER)*1000
WHERE (code='pack500' OR google_product_id='kz.aireply.pack500') AND off_android<>1;

-- New accounts get ten welcome replies once. Keep existing balances and the
-- eligibility cutoff, plus any explicitly disabled/custom administrator value.
CREATE TEMP TABLE welcome_0044_policy(align_default INTEGER NOT NULL);
INSERT INTO welcome_0044_policy
SELECT value IN ('10','20') FROM system_settings WHERE key='welcome_bonus_units';
UPDATE system_settings SET value='10',updated_at=CAST(strftime('%s','now') AS INTEGER)*1000
WHERE key='welcome_bonus_units' AND value='20';
UPDATE plans SET
 description_kk=CASE description_kk WHEN 'Жаңа тіркелгіге бір рет 20 тегін жауап.' THEN 'Жаңа тіркелгіге бір рет 10 тегін жауап.' ELSE description_kk END,
 description_ru=CASE description_ru WHEN '20 бесплатных ответов один раз для нового аккаунта.' THEN '10 бесплатных ответов один раз для нового аккаунта.' ELSE description_ru END,
 description_en=CASE description_en WHEN '20 complimentary replies once per new account.' THEN '10 complimentary replies once per new account.' ELSE description_en END,
 description_uz=CASE description_uz WHEN 'Yangi hisob uchun bir marta 20 bepul javob.' THEN 'Yangi hisob uchun bir marta 10 bepul javob.' ELSE description_uz END,
 updated_at=CAST(strftime('%s','now') AS INTEGER)*1000
WHERE code='free' AND is_free=1
 AND EXISTS(SELECT 1 FROM welcome_0044_policy WHERE align_default=1)
 AND (description_kk='Жаңа тіркелгіге бір рет 20 тегін жауап.'
  OR description_ru='20 бесплатных ответов один раз для нового аккаунта.'
  OR description_en='20 complimentary replies once per new account.'
  OR description_uz='Yangi hisob uchun bir marta 20 bepul javob.');

-- Version the current publication; old checkout snapshots remain immutable.
CREATE TEMP TABLE catalog_0044 AS
SELECT id,state,base_version,products_json AS before_json,
 (SELECT json_group_array(json(CASE
  WHEN (json_extract(value,'$.code')='pack500' OR json_extract(value,'$.google_product_id')='kz.aireply.pack500')
   AND COALESCE(json_extract(value,'$.available_android'),0)<>0
   THEN json_set(value,'$.available_android',json('false'))
  WHEN json_extract(value,'$.code')='free' AND json_extract(value,'$.units')=20
   AND EXISTS(SELECT 1 FROM welcome_0044_policy WHERE align_default=1)
   THEN json_set(value,'$.units',10)
  ELSE value END)) FROM json_each(products_json)) AS after_json
FROM billing_catalog_versions WHERE state IN ('published','draft');

INSERT INTO billing_catalog_versions
 (state,base_version,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at)
SELECT 'superseded',id,after_json,'migration:0044','migration:0044','migration:0044',
 CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000,
 CAST(strftime('%s','now') AS INTEGER)*1000
FROM catalog_0044 WHERE state='published' AND before_json<>after_json;

INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at)
SELECT id,'rebase_google_monthly','migration:0044',before_json,after_json,
 CAST(strftime('%s','now') AS INTEGER)*1000
FROM catalog_0044 WHERE state='draft' AND (before_json<>after_json OR base_version=
 (SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0044'));
UPDATE billing_catalog_versions SET
 products_json=(SELECT after_json FROM catalog_0044 WHERE id=billing_catalog_versions.id),
 base_version=CASE WHEN base_version=(SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0044')
  THEN (SELECT id FROM billing_catalog_versions WHERE created_by='migration:0044') ELSE base_version END,
 revision=revision+1,updated_by='migration:0044',updated_at=CAST(strftime('%s','now') AS INTEGER)*1000
WHERE state='draft' AND id IN (SELECT id FROM catalog_0044 WHERE before_json<>after_json OR base_version=
 (SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0044'));

UPDATE billing_catalog_versions SET state='superseded'
WHERE state='published' AND id=(SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0044');
UPDATE billing_catalog_versions SET state='published' WHERE created_by='migration:0044';
INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at)
SELECT current.id,'publish','migration:0044',previous.products_json,current.products_json,current.published_at
FROM billing_catalog_versions current JOIN billing_catalog_versions previous ON previous.id=current.base_version
WHERE current.created_by='migration:0044';
DROP TABLE catalog_0044;
DROP TABLE welcome_0044_policy;
