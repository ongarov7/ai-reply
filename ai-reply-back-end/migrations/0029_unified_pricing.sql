-- One Kazakhstan business price list for web/desktop and App Store offers.
-- App Store checkout prices still have to be configured in App Store Connect.
-- Do not rewrite sold orders, bank mandates, delivered grants, or catalog history.

UPDATE system_settings SET value = '10',
 updated_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE key = 'welcome_bonus_units';
-- Keep welcome_bonus_since and existing grants unchanged: no second welcome bonus.
UPDATE plans SET daily_message_limit = 0, monthly_message_limit = 0, entitlement_kind = 'welcome',
 description_kk = 'Жаңа тіркелгіге бір рет 10 тегін жауап.',
 description_ru = '10 бесплатных ответов один раз для нового аккаунта.',
 description_en = '10 complimentary replies once per new account.',
 description_uz = 'Yangi hisob uchun bir marta 10 bepul javob.',
 updated_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE code = 'free' AND is_free = 1;

UPDATE plans SET
 price = CASE code
  WHEN 'base_monthly' THEN 79000 WHEN 'base_yearly' THEN 790000
  WHEN 'pro_monthly' THEN 345000 WHEN 'pro_yearly' THEN 3450000 END,
 currency = 'KZT',
 daily_message_limit = CASE WHEN code IN ('base_monthly', 'base_yearly') THEN 40 ELSE 0 END,
 monthly_message_limit = 0,
 entitlement_kind = CASE WHEN code IN ('base_monthly', 'base_yearly') THEN 'daily' ELSE 'unlimited' END,
 description_kk = CASE WHEN code IN ('base_monthly', 'base_yearly') THEN 'Күніне 40 жауап' ELSE 'Шектеусіз жауаптар' END,
 description_ru = CASE WHEN code IN ('base_monthly', 'base_yearly') THEN '40 ответов в день' ELSE 'Безлимитные ответы' END,
 description_en = CASE WHEN code IN ('base_monthly', 'base_yearly') THEN '40 replies per day' ELSE 'Unlimited replies' END,
 description_uz = CASE WHEN code IN ('base_monthly', 'base_yearly') THEN 'Kuniga 40 ta javob' ELSE 'Cheksiz javoblar' END,
 updated_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE code IN ('base_monthly', 'base_yearly', 'pro_monthly', 'pro_yearly');

-- A previously customized pack may have sold a different quantity. Preserve
-- that store identity for delayed transactions and create an unmapped 30-pack.
-- An unmapped, unsold pack can safely be normalized in place.
CREATE TEMP TABLE pricing_0029_legacy_pack(id TEXT PRIMARY KEY);
INSERT INTO pricing_0029_legacy_pack
SELECT id FROM iap_products
WHERE code = 'pack30' AND units <> 30 AND
 (coalesce(apple_product_id, '') <> '' OR coalesce(google_product_id, '') <> ''
  OR EXISTS (SELECT 1 FROM iap_purchases WHERE product_code = 'pack30'));
UPDATE iap_products SET code = 'pack30_legacy_0029_' || lower(hex(randomblob(4))), is_active = 0,
 archived_at = coalesce(archived_at, CAST(strftime('%s', 'now') AS INTEGER) * 1000),
 updated_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE id IN (SELECT id FROM pricing_0029_legacy_pack);
INSERT INTO iap_products
 (id,code,kind,units,duration_hours,name_kk,name_ru,name_en,name_uz,is_active,sort_order,created_at,updated_at,price_minor,currency)
SELECT lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-8' || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6))),
 'pack30','pack',30,0,'30 қосымша жауап','30 дополнительных ответов','30 extra replies','30 ta qo‘shimcha javob',0,40,
 CAST(strftime('%s', 'now') AS INTEGER) * 1000, CAST(strftime('%s', 'now') AS INTEGER) * 1000,13000,'KZT'
WHERE EXISTS (SELECT 1 FROM pricing_0029_legacy_pack);
UPDATE iap_products SET units = 30 WHERE code = 'pack30' AND units <> 30;

UPDATE iap_products SET price_minor = CASE code
 WHEN 'base_monthly' THEN 79000 WHEN 'base_yearly' THEN 790000
 WHEN 'pro_monthly' THEN 345000 WHEN 'pro_yearly' THEN 3450000
 WHEN 'pack30' THEN 13000 END,
 currency = 'KZT', updated_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE code IN ('base_monthly', 'base_yearly', 'pro_monthly', 'pro_yearly', 'pack30');

-- Preserve mapped store IDs, localized names, and sales availability. Build a
-- fresh version instead of mutating snapshots referenced by pending checkouts.
INSERT INTO billing_catalog_versions
 (state,base_version,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at)
SELECT 'superseded', id,
 (SELECT json_group_array(json(CASE json_extract(value, '$.code')
   WHEN 'free' THEN json_set(value, '$.price_minor', 0, '$.units', 10, '$.daily_limit', 0, '$.unlimited', json('false'))
   WHEN 'base_monthly' THEN json_set(value, '$.price_minor', 79000, '$.daily_limit', 40, '$.unlimited', json('false'))
   WHEN 'base_yearly' THEN json_set(value, '$.price_minor', 790000, '$.daily_limit', 40, '$.unlimited', json('false'))
   WHEN 'pro_monthly' THEN json_set(value, '$.price_minor', 345000, '$.daily_limit', 0, '$.unlimited', json('true'))
   WHEN 'pro_yearly' THEN json_set(value, '$.price_minor', 3450000, '$.daily_limit', 0, '$.unlimited', json('true'))
   WHEN 'pack30' THEN CASE WHEN EXISTS (SELECT 1 FROM pricing_0029_legacy_pack)
    THEN json_set(value, '$.price_minor', 13000, '$.units', 30, '$.apple_product_id', '', '$.available_ios', json('false'))
    ELSE json_set(value, '$.price_minor', 13000, '$.units', 30) END
   ELSE value END)) FROM json_each(products_json)),
 'migration:0029', 'migration:0029', 'migration:0029',
 CAST(strftime('%s', 'now') AS INTEGER) * 1000,
 CAST(strftime('%s', 'now') AS INTEGER) * 1000,
 CAST(strftime('%s', 'now') AS INTEGER) * 1000
FROM billing_catalog_versions WHERE state = 'published';

-- Retain old drafts in history. They must not restore the retired prices after
-- this release; the next draft starts from the newly published version.
INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at)
SELECT id, 'supersede_draft', 'migration:0029', products_json, products_json,
 CAST(strftime('%s', 'now') AS INTEGER) * 1000
FROM billing_catalog_versions WHERE state = 'draft';
UPDATE billing_catalog_versions SET state = 'superseded'
WHERE state IN ('published', 'draft')
 AND EXISTS (SELECT 1 FROM billing_catalog_versions WHERE created_by = 'migration:0029');
UPDATE billing_catalog_versions SET state = 'published' WHERE created_by = 'migration:0029';

INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at)
SELECT current.id, 'publish', 'migration:0029', previous.products_json, current.products_json, current.published_at
FROM billing_catalog_versions current JOIN billing_catalog_versions previous ON previous.id = current.base_version
WHERE current.created_by = 'migration:0029';

DROP TABLE pricing_0029_legacy_pack;
