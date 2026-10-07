-- 0011_push_notifications: push хабарламалары — орнатулар тізілімі, outbox (push
-- және пошта), науқандар, санат баптаулары және қолданушы таңдаған тіл.
--
-- Only additive changes: new tables, new columns on users and on the
-- notification_campaigns stub that no code ever wrote to, and indexes.
-- Existing rows are untouched. users.preferred_language starts empty ("not
-- chosen"): notifications keep falling back to the device and account locale
-- until the app sends the person's choice. All times are UTC unix
-- milliseconds, as everywhere else.
--
-- Number 0011 on purpose: 0007–0008 were skipped on main. This file replaces
-- the push schema of the unmerged notification branch (its 0006 and 0008):
-- FCM for both platforms, an e-mail channel in the same outbox, campaign
-- texts per language.
-- Rollback: older binaries never touch the new tables or columns, so deploying
-- the previous build is enough. By hand: DROP TABLE notification_deliveries,
-- notifications, notification_preferences, app_installations; the added
-- columns can go with ALTER TABLE … DROP COLUMN (SQLite 3.35+).

-- Қолданушы өзі таңдаған тіл: kk | ru | en | uz, '' — таңдалмаған.
ALTER TABLE users ADD COLUMN preferred_language TEXT NOT NULL DEFAULT '';

-- Қосымшаның бір орнатуы. installation_id-ді қосымша өзі жасайды; user_id бос
-- болса — орнату анонимді (кірмеген). Push токені тек шифрланған түрде сақталады,
-- бірегейлік пен журнал үшін оның SHA-256 хэші бар.
CREATE TABLE app_installations (
    id                    TEXT PRIMARY KEY,
    installation_id       TEXT NOT NULL UNIQUE,
    user_id               TEXT REFERENCES users (id) ON DELETE SET NULL,
    platform              TEXT NOT NULL,                     -- ios | android
    app_version           TEXT NOT NULL DEFAULT '',
    app_version_num       INTEGER NOT NULL DEFAULT 0,        -- 1.3.2 → 1003002
    app_build             TEXT NOT NULL DEFAULT '',
    os_name               TEXT NOT NULL DEFAULT '',
    os_version            TEXT NOT NULL DEFAULT '',
    os_version_num        INTEGER NOT NULL DEFAULT 0,
    device_model          TEXT NOT NULL DEFAULT '',
    manufacturer          TEXT NOT NULL DEFAULT '',
    locale                TEXT NOT NULL DEFAULT '',
    timezone              TEXT NOT NULL DEFAULT '',
    push_provider         TEXT NOT NULL DEFAULT 'fcm',       -- екі платформа да FCM арқылы
    push_token_sealed     TEXT,                              -- AES-GCM, never returned by any API
    push_token_hash       TEXT,                              -- sha256(token), hex
    push_permission       TEXT NOT NULL DEFAULT 'unknown',   -- authorized | denied | not_determined | provisional | ephemeral | unknown
    notifications_enabled INTEGER NOT NULL DEFAULT 1,        -- the in-app switch
    push_status           TEXT NOT NULL DEFAULT 'none',      -- none | active | invalid | replaced
    push_status_reason    TEXT NOT NULL DEFAULT '',
    token_updated_at      INTEGER,
    push_disabled_at      INTEGER,
    attached_at           INTEGER,
    first_seen_at         INTEGER NOT NULL,
    last_seen_at          INTEGER NOT NULL,
    created_at            INTEGER NOT NULL,
    updated_at            INTEGER NOT NULL
);
-- Бір токен бір ғана орнатуға тиесілі.
CREATE UNIQUE INDEX idx_installations_token
    ON app_installations (push_provider, push_token_hash) WHERE push_token_hash IS NOT NULL;
CREATE INDEX idx_installations_user ON app_installations (user_id);
CREATE INDEX idx_installations_versions ON app_installations (platform, app_version, app_build);
CREATE INDEX idx_installations_last_seen ON app_installations (last_seen_at);
CREATE INDEX idx_installations_push ON app_installations (push_status, platform, last_seen_at);

-- Санат бойынша қолданушы баптауы. Жол жоқ болса — санат қосулы.
CREATE TABLE notification_preferences (
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    category   TEXT NOT NULL,                                -- account | subscription | system | marketing
    enabled    INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, category)
);

-- 0001-дегі бос науқан кестесі толық науқанға айналады. title/body — қор
-- тілдегі мәтін; әр тілдің мәтіні content-те.
ALTER TABLE notification_campaigns ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_campaigns ADD COLUMN category TEXT NOT NULL DEFAULT 'marketing';
ALTER TABLE notification_campaigns ADD COLUMN link TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_campaigns ADD COLUMN data TEXT NOT NULL DEFAULT '{}';
ALTER TABLE notification_campaigns ADD COLUMN audience_filter TEXT NOT NULL DEFAULT '{}';
ALTER TABLE notification_campaigns ADD COLUMN content TEXT NOT NULL DEFAULT '{}';      -- {"kk":{"title","body"},…}
ALTER TABLE notification_campaigns ADD COLUMN fallback_locale TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_campaigns ADD COLUMN idempotency_key TEXT;
ALTER TABLE notification_campaigns ADD COLUMN recipient_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE notification_campaigns ADD COLUMN device_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE notification_campaigns ADD COLUMN queued_at INTEGER;
ALTER TABLE notification_campaigns ADD COLUMN started_at INTEGER;
ALTER TABLE notification_campaigns ADD COLUMN completed_at INTEGER;
ALTER TABLE notification_campaigns ADD COLUMN cancelled_at INTEGER;
ALTER TABLE notification_campaigns ADD COLUMN final_stats TEXT NOT NULL DEFAULT '';
-- Бір Idempotency-Key — бір науқан (қос басу мен қайталанған сұраныс жаңасын ашпайды).
CREATE UNIQUE INDEX idx_campaigns_idempotency
    ON notification_campaigns (idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX idx_campaigns_status ON notification_campaigns (status, created_at);

-- Логикалық хабарлама: науқанның бір тілі не бір қолданушыға бір оқиға.
-- dedupe_key бірегей: бір оқиғаның екінші рет келуі жаңа жол ашпайды.
-- params — пошта үлгісінің мәндері; тек серверде, құрылғыға ешқашан жіберілмейді.
CREATE TABLE notifications (
    id              TEXT PRIMARY KEY,
    dedupe_key      TEXT NOT NULL UNIQUE,                     -- campaign:<id>:<locale> | user:<user_id>:<key>
    idempotency_key TEXT NOT NULL,
    campaign_id     TEXT REFERENCES notification_campaigns (id) ON DELETE CASCADE,
    user_id         TEXT REFERENCES users (id) ON DELETE CASCADE,
    category        TEXT NOT NULL,
    type            TEXT NOT NULL,
    locale          TEXT NOT NULL DEFAULT '',
    title           TEXT NOT NULL,
    body            TEXT NOT NULL,
    link            TEXT NOT NULL DEFAULT '',
    data            TEXT NOT NULL DEFAULT '{}',
    params          TEXT NOT NULL DEFAULT '{}',
    created_at      INTEGER NOT NULL
);
CREATE INDEX idx_notifications_user ON notifications (user_id, created_at);
CREATE INDEX idx_notifications_campaign ON notifications (campaign_id);
CREATE INDEX idx_notifications_created ON notifications (created_at);

-- Transactional outbox: бір хабарламаның бір арнамен жеткізілуі.
-- channel = push: бір орнатуға бір жол; installation_id бос болса — push
-- жіберілмегені туралы жалғыз skipped жол (push өшірулі не құрылғы жоқ).
-- channel = email: хабарламаға бір хат, мекенжай жіберу сәтінде оқылады.
-- Жұмысшы жолды lease арқылы алады (lease_owner, lease_until).
CREATE TABLE notification_deliveries (
    id                  TEXT PRIMARY KEY,
    notification_id     TEXT NOT NULL REFERENCES notifications (id) ON DELETE CASCADE,
    campaign_id         TEXT REFERENCES notification_campaigns (id) ON DELETE CASCADE,
    channel             TEXT NOT NULL DEFAULT 'push',   -- push | email
    installation_id     TEXT REFERENCES app_installations (id) ON DELETE CASCADE,
    user_id             TEXT REFERENCES users (id) ON DELETE SET NULL,  -- owner when queued
    platform            TEXT NOT NULL DEFAULT '',
    provider            TEXT NOT NULL DEFAULT '',       -- fcm | resend
    status              TEXT NOT NULL,                  -- queued | sending | retrying | provider_accepted | provider_failed | invalid_token | skipped | cancelled
    attempt_count       INTEGER NOT NULL DEFAULT 0,
    next_attempt_at     INTEGER NOT NULL,
    lease_owner         TEXT NOT NULL DEFAULT '',
    lease_until         INTEGER,
    provider_message_id TEXT NOT NULL DEFAULT '',
    token_fingerprint   TEXT NOT NULL DEFAULT '',       -- push: the token this send used (fcm:1a2b3c4d), never the token
    error_code          TEXT NOT NULL DEFAULT '',
    error_detail        TEXT NOT NULL DEFAULT '',
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    sent_at             INTEGER,
    failed_at           INTEGER,
    opened_at           INTEGER
);
-- Бір хабарлама бір құрылғыға бір-ақ рет, бір хат бір-ақ рет, skipped жол бір-ақ рет.
CREATE UNIQUE INDEX idx_deliveries_device
    ON notification_deliveries (notification_id, installation_id) WHERE installation_id IS NOT NULL;
CREATE UNIQUE INDEX idx_deliveries_email
    ON notification_deliveries (notification_id) WHERE channel = 'email';
CREATE UNIQUE INDEX idx_deliveries_skipped_push
    ON notification_deliveries (notification_id) WHERE channel = 'push' AND installation_id IS NULL;
CREATE INDEX idx_deliveries_due ON notification_deliveries (status, next_attempt_at);
CREATE INDEX idx_deliveries_installation ON notification_deliveries (installation_id, created_at);
CREATE INDEX idx_deliveries_user ON notification_deliveries (user_id, created_at);
CREATE INDEX idx_deliveries_created ON notification_deliveries (created_at);
-- Науқан есебі мен оның жеткізу беті индекстің өзінен оқылады.
CREATE INDEX idx_deliveries_campaign_stats
    ON notification_deliveries (campaign_id, notification_id, status, platform, opened_at);
CREATE INDEX idx_deliveries_campaign_created
    ON notification_deliveries (campaign_id, created_at DESC, id);

-- Мерзімі бітуге жақын және біткен жазылымдар (15 минут сайынғы тексеру және еске салулар).
-- Тегін жазылымның мерзімі жоқ, сондықтан индекс тек мерзімі бар жолдарды ұстайды.
CREATE INDEX idx_subscriptions_expiry ON subscriptions (expires_at) WHERE expires_at IS NOT NULL;
