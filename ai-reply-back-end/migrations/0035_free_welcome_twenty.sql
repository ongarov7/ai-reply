-- Free gives 20 replies once at registration, not a daily allowance.
-- Only the prior default changes; an administrator's disabled/custom setting
-- remains authoritative. Preserve welcome_bonus_since so an eligible missed
-- registration grant can still be repaired by the existing once-only flow.
-- Never change existing grants, balances, usage, paid prices or sold snapshots.
CREATE TEMP TABLE welcome_0035_policy(align_default INTEGER NOT NULL);
INSERT INTO welcome_0035_policy
SELECT CASE WHEN value IN ('10','20') THEN 1 ELSE 0 END
FROM system_settings WHERE key='welcome_bonus_units';

UPDATE system_settings SET value='20',
 updated_at=CAST(strftime('%s','now') AS INTEGER)*1000
WHERE key='welcome_bonus_units' AND value='10';

-- Change only exact old default copy; preserve administrator descriptions.
UPDATE plans SET
 description_kk=CASE description_kk WHEN 'Жаңа тіркелгіге бір рет 10 тегін жауап.' THEN 'Жаңа тіркелгіге бір рет 20 тегін жауап.' ELSE description_kk END,
 description_ru=CASE description_ru WHEN '10 бесплатных ответов один раз для нового аккаунта.' THEN '20 бесплатных ответов один раз для нового аккаунта.' ELSE description_ru END,
 description_en=CASE description_en WHEN '10 complimentary replies once per new account.' THEN '20 complimentary replies once per new account.' ELSE description_en END,
 description_uz=CASE description_uz WHEN 'Yangi hisob uchun bir marta 10 bepul javob.' THEN 'Yangi hisob uchun bir marta 20 bepul javob.' ELSE description_uz END,
 updated_at=CAST(strftime('%s','now') AS INTEGER)*1000
WHERE code='free' AND is_free=1
 AND EXISTS(SELECT 1 FROM welcome_0035_policy WHERE align_default=1)
 AND (description_kk='Жаңа тіркелгіге бір рет 10 тегін жауап.'
  OR description_ru='10 бесплатных ответов один раз для нового аккаунта.'
  OR description_en='10 complimentary replies once per new account.'
  OR description_uz='Yangi hisob uchun bir marta 10 bepul javob.');

-- Clone the current publication, changing only Free's old default quantity.
-- Existing orders and mandates keep referencing their original immutable JSON.
INSERT INTO billing_catalog_versions
 (state,base_version,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at)
SELECT 'superseded',id,
 (SELECT json_group_array(json(CASE
  WHEN json_extract(value,'$.code')='free' AND json_extract(value,'$.units')=10
  THEN json_set(value,'$.units',20) ELSE value END)) FROM json_each(products_json)),
 'migration:0035','migration:0035','migration:0035',
 CAST(strftime('%s','now') AS INTEGER)*1000,
 CAST(strftime('%s','now') AS INTEGER)*1000,
 CAST(strftime('%s','now') AS INTEGER)*1000
FROM billing_catalog_versions
WHERE state='published'
 AND EXISTS(SELECT 1 FROM welcome_0035_policy WHERE align_default=1)
 AND EXISTS(SELECT 1 FROM json_each(products_json)
  WHERE json_extract(value,'$.code')='free' AND json_extract(value,'$.units')=10);

-- Retain unrelated draft edits/custom quantities and rebase only a draft of
-- the replaced publication. Increment revision to reject stale browser saves.
INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at)
SELECT id,'rebase_welcome','migration:0035',products_json,
 (SELECT json_group_array(json(CASE
  WHEN json_extract(value,'$.code')='free' AND json_extract(value,'$.units')=10
  THEN json_set(value,'$.units',20) ELSE value END)) FROM json_each(products_json)),
 CAST(strftime('%s','now') AS INTEGER)*1000
FROM billing_catalog_versions
WHERE state='draft'
 AND EXISTS(SELECT 1 FROM welcome_0035_policy WHERE align_default=1)
 AND (EXISTS(SELECT 1 FROM json_each(products_json)
   WHERE json_extract(value,'$.code')='free' AND json_extract(value,'$.units')=10)
  OR base_version=(SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0035'));

UPDATE billing_catalog_versions SET
 products_json=(SELECT json_group_array(json(CASE
  WHEN json_extract(value,'$.code')='free' AND json_extract(value,'$.units')=10
  THEN json_set(value,'$.units',20) ELSE value END)) FROM json_each(products_json)),
 base_version=CASE
  WHEN base_version=(SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0035')
  THEN (SELECT id FROM billing_catalog_versions WHERE created_by='migration:0035') ELSE base_version END,
 revision=revision+1,updated_by='migration:0035',
 updated_at=CAST(strftime('%s','now') AS INTEGER)*1000
WHERE state='draft'
 AND EXISTS(SELECT 1 FROM welcome_0035_policy WHERE align_default=1)
 AND (EXISTS(SELECT 1 FROM json_each(products_json)
   WHERE json_extract(value,'$.code')='free' AND json_extract(value,'$.units')=10)
  OR base_version=(SELECT base_version FROM billing_catalog_versions WHERE created_by='migration:0035'));

UPDATE billing_catalog_versions SET state='superseded'
WHERE state='published' AND EXISTS(SELECT 1 FROM billing_catalog_versions WHERE created_by='migration:0035');
UPDATE billing_catalog_versions SET state='published' WHERE created_by='migration:0035';
INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at)
SELECT current.id,'publish','migration:0035',previous.products_json,current.products_json,current.published_at
FROM billing_catalog_versions current
JOIN billing_catalog_versions previous ON previous.id=current.base_version
WHERE current.created_by='migration:0035';

DROP TABLE welcome_0035_policy;
