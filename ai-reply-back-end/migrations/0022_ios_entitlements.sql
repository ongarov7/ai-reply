-- Approved iOS entitlement model. Existing grants and paid subscriptions remain intact.
ALTER TABLE plans ADD COLUMN entitlement_kind TEXT NOT NULL DEFAULT 'daily';
ALTER TABLE iap_products ADD COLUMN plan_id TEXT NOT NULL DEFAULT '';
ALTER TABLE iap_products ADD COLUMN billing_period TEXT NOT NULL DEFAULT '';
ALTER TABLE iap_purchases ADD COLUMN price_milliunits INTEGER NOT NULL DEFAULT 0;
ALTER TABLE iap_purchases ADD COLUMN currency TEXT NOT NULL DEFAULT '';

-- No welcome regrant for an existing account, including accounts with an exhausted grant.
UPDATE system_settings SET value = '10', updated_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE key = 'welcome_bonus_units';
UPDATE system_settings SET value = CAST(MAX(CAST(strftime('%s', 'now') AS INTEGER) * 1000, COALESCE((SELECT MAX(created_at) + 1 FROM users), 0)) AS TEXT),
 updated_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000 WHERE key = 'welcome_bonus_since';
UPDATE plans SET daily_message_limit = 0, monthly_message_limit = 0, entitlement_kind = 'welcome',
 description_kk = 'Жаңа тіркелгіге бір рет 10 тегін жауап.',
 description_ru = '10 бесплатных ответов один раз для нового аккаунта.',
 description_en = '10 complimentary replies once per new account.',
 description_uz = 'Yangi hisob uchun bir marta 10 bepul javob.' WHERE code = 'free' AND is_free = 1;
UPDATE system_settings SET value='free' WHERE key='default_plan_code';
-- Keep legacy paid contracts usable, but do not offer them to new buyers.
UPDATE plans SET is_visible = 0 WHERE code IN ('standard', 'pro') AND is_free = 0;

INSERT OR IGNORE INTO plans
 (id,code,name_kk,name_ru,name_en,name_uz,description_kk,description_ru,description_en,description_uz,
 price,currency,daily_message_limit,monthly_message_limit,period_days,is_free,is_active,sort_order,
 created_at,updated_at,is_visible,entitlement_kind)
VALUES
 ('00000000-0000-4000-8000-000000000101','base_monthly','Base','Base','Base','Base','Күніне 30 жауап','30 ответов в день','30 replies per day','Kuniga 30 ta javob',59900,'KZT',30,0,30,0,1,20,0,0,1,'daily'),
 ('00000000-0000-4000-8000-000000000102','base_yearly','Base','Base','Base','Base','Күніне 30 жауап','30 ответов в день','30 replies per day','Kuniga 30 ta javob',599900,'KZT',30,0,365,0,1,21,0,0,1,'daily'),
 ('00000000-0000-4000-8000-000000000103','pro_monthly','Pro','Pro','Pro','Pro','Шектеусіз жауаптар','Безлимитные ответы','Unlimited replies','Cheksiz javoblar',299900,'KZT',0,0,30,0,1,30,0,0,1,'unlimited'),
 ('00000000-0000-4000-8000-000000000104','pro_yearly','Pro','Pro','Pro','Pro','Шектеусіз жауаптар','Безлимитные ответы','Unlimited replies','Cheksiz javoblar',2999000,'KZT',0,0,365,0,1,31,0,0,1,'unlimited');

-- No invented store IDs. Sales stay off until real App Store products are mapped and enabled.
-- Retire previous offers from new sales only; their IDs and all delivered/pending purchases remain valid.
UPDATE iap_products SET is_active = 0;
INSERT OR IGNORE INTO iap_products
 (id,code,kind,units,duration_hours,name_kk,name_ru,name_en,name_uz,is_active,sort_order,created_at,updated_at,plan_id,billing_period)
VALUES
 ('00000000-0000-4000-8000-000000000201','base_monthly','subscription',0,0,'Base · ай','Base · месяц','Base · monthly','Base · oy',0,20,0,0,'00000000-0000-4000-8000-000000000101','month'),
 ('00000000-0000-4000-8000-000000000202','base_yearly','subscription',0,0,'Base · жыл','Base · год','Base · yearly','Base · yil',0,21,0,0,'00000000-0000-4000-8000-000000000102','year'),
 ('00000000-0000-4000-8000-000000000203','pro_monthly','subscription',0,0,'Pro · ай','Pro · месяц','Pro · monthly','Pro · oy',0,30,0,0,'00000000-0000-4000-8000-000000000103','month'),
 ('00000000-0000-4000-8000-000000000204','pro_yearly','subscription',0,0,'Pro · жыл','Pro · год','Pro · yearly','Pro · yil',0,31,0,0,'00000000-0000-4000-8000-000000000104','year'),
 ('00000000-0000-4000-8000-000000000205','pack30','pack',30,0,'30 қосымша жауап','30 дополнительных ответов','30 extra replies','30 ta qo‘shimcha javob',0,40,0,0,'','');

CREATE TABLE iap_account_tokens (
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 token TEXT NOT NULL UNIQUE COLLATE NOCASE
);

-- Store-derived subscription state is separate from legacy/manual subscriptions.
CREATE TABLE apple_subscriptions (
 original_transaction_id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 transaction_id TEXT NOT NULL,
 product_id TEXT NOT NULL,
 plan_id TEXT NOT NULL REFERENCES plans(id),
 environment TEXT NOT NULL,
 purchased_at INTEGER NOT NULL,
 expires_at INTEGER NOT NULL,
 signed_at INTEGER NOT NULL,
 revoked_at INTEGER,
 auto_renew INTEGER NOT NULL DEFAULT 1,
 updated_at INTEGER NOT NULL
);
CREATE INDEX idx_apple_subscriptions_user ON apple_subscriptions(user_id, expires_at);
