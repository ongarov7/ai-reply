-- Preserve the terms of already purchased Base periods. Only purchases made
-- after this rollout receive the new monthly bundle. Existing daily rollover
-- periods, balances, orders, bank mandates and signed amounts are untouched.
INSERT OR IGNORE INTO system_settings(key,value,updated_at)
SELECT 'apple_base_monthly_since', CAST(strftime('%s','now') AS INTEGER)*1000,
 CAST(strftime('%s','now') AS INTEGER)*1000;
INSERT OR IGNORE INTO system_settings(key,value,updated_at)
SELECT 'apple_base_legacy_daily_units', COALESCE(NULLIF(daily_message_limit,0),40),
 CAST(strftime('%s','now') AS INTEGER)*1000 FROM plans WHERE code='base_monthly';

UPDATE plans SET price=99000,daily_message_limit=0,monthly_message_limit=1000,
 entitlement_kind='monthly',
 description_ru='1 000 ответов каждый оплаченный месяц. Остаток не сгорает.',
 description_en='1,000 replies each paid month. Unused replies never expire.',
 description_kk='Әр төленген айға 1 000 жауап. Қалған жауаптардың мерзімі шектелмейді.',
 description_uz='Har to‘langan oy uchun 1 000 javob. Qolgan javoblar muddatsiz.',
 updated_at=CAST(strftime('%s','now') AS INTEGER)*1000 WHERE code='base_monthly';
UPDATE plans SET price=CASE code WHEN 'pro_monthly' THEN 399000 ELSE 3999000 END,
 updated_at=CAST(strftime('%s','now') AS INTEGER)*1000 WHERE code IN ('pro_monthly','pro_yearly');
-- Keep the former annual Base for existing customers, outside the new lineup.
UPDATE plans SET is_visible=0 WHERE code='base_yearly';
UPDATE iap_products SET price_minor=CASE code
 WHEN 'base_monthly' THEN 99000 WHEN 'pro_monthly' THEN 399000 ELSE 3999000 END,
 updated_at=CAST(strftime('%s','now') AS INTEGER)*1000
 WHERE code IN ('base_monthly','pro_monthly','pro_yearly');
UPDATE iap_products SET is_active=0 WHERE code='base_yearly';

-- Transform only the approved fields. Retain names, store IDs, welcome policy,
-- other prices and the owner's unrelated draft edits.
CREATE TEMP TABLE release_0037_catalog AS
SELECT id,state,base_version,products_json AS before_json,
 (SELECT json_group_array(json(CASE json_extract(value,'$.code')
  WHEN 'base_monthly' THEN json_set(value,'$.price_minor',99000,'$.daily_limit',0,'$.monthly_limit',1000)
  WHEN 'base_yearly' THEN json_set(value,'$.active',json('false'),'$.available_ios',json('false'),'$.available_web',json('false'))
  WHEN 'pro_monthly' THEN json_set(value,'$.price_minor',399000)
  WHEN 'pro_yearly' THEN json_set(value,'$.price_minor',3999000)
  ELSE value END)) FROM json_each(products_json)) AS after_json,
 EXISTS(SELECT 1 FROM json_each(products_json) WHERE
  (json_extract(value,'$.code')='base_monthly' AND
   (json_extract(value,'$.price_minor')<>99000 OR json_extract(value,'$.daily_limit')<>0 OR COALESCE(json_extract(value,'$.monthly_limit'),0)<>1000)) OR
  (json_extract(value,'$.code')='base_yearly' AND
   (json_extract(value,'$.active')<>0 OR json_extract(value,'$.available_ios')<>0 OR json_extract(value,'$.available_web')<>0)) OR
  (json_extract(value,'$.code')='pro_monthly' AND json_extract(value,'$.price_minor')<>399000) OR
  (json_extract(value,'$.code')='pro_yearly' AND json_extract(value,'$.price_minor')<>3999000)) AS changed
FROM billing_catalog_versions WHERE state IN ('published','draft');

INSERT INTO billing_catalog_versions
 (state,base_version,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at)
SELECT 'superseded',id,after_json,'migration:0037','migration:0037','migration:0037',
 CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000,
 CAST(strftime('%s','now') AS INTEGER)*1000
FROM release_0037_catalog WHERE state='published' AND changed;

INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at)
SELECT id,'rebase_allowance','migration:0037',before_json,after_json,CAST(strftime('%s','now') AS INTEGER)*1000
FROM release_0037_catalog WHERE state='draft' AND (changed OR base_version=
 (SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0037'));
UPDATE billing_catalog_versions SET
 products_json=(SELECT after_json FROM release_0037_catalog WHERE id=billing_catalog_versions.id),
 base_version=CASE WHEN base_version=(SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0037')
  THEN (SELECT id FROM billing_catalog_versions WHERE created_by='migration:0037') ELSE base_version END,
 revision=revision+1,updated_by='migration:0037',updated_at=CAST(strftime('%s','now') AS INTEGER)*1000
WHERE state='draft' AND id IN (SELECT id FROM release_0037_catalog WHERE changed OR base_version=
 (SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0037'));

UPDATE billing_catalog_versions SET state='superseded'
WHERE state='published' AND id=(SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0037');
UPDATE billing_catalog_versions SET state='published' WHERE created_by='migration:0037';
INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at)
SELECT current.id,'publish','migration:0037',previous.products_json,current.products_json,current.published_at
FROM billing_catalog_versions current JOIN billing_catalog_versions previous ON previous.id=current.base_version
WHERE current.created_by='migration:0037';
DROP TABLE release_0037_catalog;
