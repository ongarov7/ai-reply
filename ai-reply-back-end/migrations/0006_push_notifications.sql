-- 0006_push_notifications: орнатулар тізілімі, push outbox, науқандар, баптаулар.
--
-- Only additive changes: new tables, new columns on the notification_campaigns
-- stub that no code ever wrote to, and indexes. Existing rows are untouched.
-- All times are UTC unix milliseconds, as everywhere else.

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
    push_provider         TEXT NOT NULL DEFAULT '',          -- fcm | apns
    push_environment      TEXT NOT NULL DEFAULT '',          -- apns: sandbox | production
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

-- 0001-дегі бос науқан кестесі толық науқанға айналады.
ALTER TABLE notification_campaigns ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_campaigns ADD COLUMN category TEXT NOT NULL DEFAULT 'marketing';
ALTER TABLE notification_campaigns ADD COLUMN link TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_campaigns ADD COLUMN data TEXT NOT NULL DEFAULT '{}';
ALTER TABLE notification_campaigns ADD COLUMN audience_filter TEXT NOT NULL DEFAULT '{}';
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

-- Логикалық хабарлама: бір науқан не бір қолданушыға бір оқиға.
-- dedupe_key бірегей: бір оқиғаның екінші рет келуі жаңа жол ашпайды.
CREATE TABLE notifications (
    id              TEXT PRIMARY KEY,
    dedupe_key      TEXT NOT NULL UNIQUE,                     -- campaign:<id> | user:<user_id>:<idempotency_key>
    idempotency_key TEXT NOT NULL,
    campaign_id     TEXT REFERENCES notification_campaigns (id) ON DELETE CASCADE,
    user_id         TEXT REFERENCES users (id) ON DELETE CASCADE,
    category        TEXT NOT NULL,
    type            TEXT NOT NULL,
    title           TEXT NOT NULL,
    body            TEXT NOT NULL,
    link            TEXT NOT NULL DEFAULT '',
    data            TEXT NOT NULL DEFAULT '{}',
    created_at      INTEGER NOT NULL
);
CREATE INDEX idx_notifications_user ON notifications (user_id, created_at);
CREATE INDEX idx_notifications_campaign ON notifications (campaign_id);
CREATE INDEX idx_notifications_created ON notifications (created_at);

-- Transactional outbox: бір хабарламаның бір орнатуға жеткізілуі.
-- UNIQUE (notification_id, installation_id): бір хабарлама бір құрылғыға бір-ақ рет
-- кезекке тұрады. Жұмысшы жолды lease арқылы алады (lease_owner, lease_until).
CREATE TABLE notification_deliveries (
    id                  TEXT PRIMARY KEY,
    notification_id     TEXT NOT NULL REFERENCES notifications (id) ON DELETE CASCADE,
    campaign_id         TEXT REFERENCES notification_campaigns (id) ON DELETE CASCADE,
    installation_id     TEXT NOT NULL REFERENCES app_installations (id) ON DELETE CASCADE,
    user_id             TEXT REFERENCES users (id) ON DELETE SET NULL,  -- owner when queued
    platform            TEXT NOT NULL,
    provider            TEXT NOT NULL,
    status              TEXT NOT NULL,          -- queued | sending | retrying | provider_accepted | provider_failed | invalid_token | skipped | cancelled
    attempt_count       INTEGER NOT NULL DEFAULT 0,
    next_attempt_at     INTEGER NOT NULL,
    lease_owner         TEXT NOT NULL DEFAULT '',
    lease_until         INTEGER,
    provider_message_id TEXT NOT NULL DEFAULT '',
    error_code          TEXT NOT NULL DEFAULT '',
    error_detail        TEXT NOT NULL DEFAULT '',
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    sent_at             INTEGER,
    failed_at           INTEGER,
    opened_at           INTEGER,
    UNIQUE (notification_id, installation_id)
);
CREATE INDEX idx_deliveries_due ON notification_deliveries (status, next_attempt_at);
CREATE INDEX idx_deliveries_campaign ON notification_deliveries (campaign_id, status);
CREATE INDEX idx_deliveries_installation ON notification_deliveries (installation_id, created_at);
CREATE INDEX idx_deliveries_user ON notification_deliveries (user_id, created_at);
CREATE INDEX idx_deliveries_created ON notification_deliveries (created_at);
