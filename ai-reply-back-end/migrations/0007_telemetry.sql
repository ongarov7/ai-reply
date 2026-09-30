-- 0007_telemetry: қосымша сессиялары мен оқиғалары, кіру оқиғалары, API қателері.
--
-- Only additive changes. No column here can hold a message, a reply, a code,
-- a token or a password: the ingestion code accepts an allowlist of event
-- names and typed properties, and nothing else.
-- installation_id in these tables is the app-generated id (join on
-- app_installations.installation_id), so a telemetry row never blocks the
-- deletion of an installation.

CREATE TABLE app_sessions (
    id               TEXT PRIMARY KEY,
    session_id       TEXT NOT NULL UNIQUE,                 -- app-generated
    installation_id  TEXT NOT NULL DEFAULT '',
    user_id          TEXT REFERENCES users (id) ON DELETE SET NULL,
    platform         TEXT NOT NULL DEFAULT '',
    app_version      TEXT NOT NULL DEFAULT '',
    app_build        TEXT NOT NULL DEFAULT '',
    os_version       TEXT NOT NULL DEFAULT '',
    device_model     TEXT NOT NULL DEFAULT '',
    started_at       INTEGER NOT NULL,
    last_activity_at INTEGER NOT NULL,
    ended_at         INTEGER,
    event_count      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_app_sessions_user ON app_sessions (user_id, started_at);
CREATE INDEX idx_app_sessions_installation ON app_sessions (installation_id, started_at);
CREATE INDEX idx_app_sessions_activity ON app_sessions (last_activity_at);

CREATE TABLE app_events (
    id              TEXT PRIMARY KEY,
    client_event_id TEXT,                                  -- lets a retried batch be ignored
    event_name      TEXT NOT NULL,
    user_id         TEXT REFERENCES users (id) ON DELETE CASCADE,
    installation_id TEXT NOT NULL DEFAULT '',
    session_id      TEXT NOT NULL DEFAULT '',
    platform        TEXT NOT NULL DEFAULT '',
    app_version     TEXT NOT NULL DEFAULT '',
    app_build       TEXT NOT NULL DEFAULT '',
    os_version      TEXT NOT NULL DEFAULT '',
    device_model    TEXT NOT NULL DEFAULT '',
    outcome         TEXT NOT NULL DEFAULT '',              -- success | failure | ''
    error_code      TEXT NOT NULL DEFAULT '',
    request_id      TEXT NOT NULL DEFAULT '',
    properties      TEXT NOT NULL DEFAULT '{}',            -- validated, bounded JSON
    occurred_at     INTEGER NOT NULL,
    received_at     INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_app_events_client
    ON app_events (installation_id, client_event_id) WHERE client_event_id IS NOT NULL;
CREATE INDEX idx_app_events_occurred ON app_events (occurred_at);
CREATE INDEX idx_app_events_name ON app_events (event_name, occurred_at);
CREATE INDEX idx_app_events_user ON app_events (user_id, occurred_at);
CREATE INDEX idx_app_events_installation ON app_events (installation_id, occurred_at);
CREATE INDEX idx_app_events_build ON app_events (platform, app_build, event_name);

-- Кіру оқиғалары: қауіпсіздік журналы, бөлек сақтау мерзімімен.
CREATE TABLE auth_events (
    id              TEXT PRIMARY KEY,
    event_name      TEXT NOT NULL,
    method          TEXT NOT NULL DEFAULT '',              -- email | google | apple | phone | refresh
    outcome         TEXT NOT NULL,                         -- success | failure
    error_code      TEXT NOT NULL DEFAULT '',
    user_id         TEXT REFERENCES users (id) ON DELETE CASCADE,
    subject_hash    TEXT NOT NULL DEFAULT '',              -- HMAC of the e-mail/phone the attempt named
    installation_id TEXT NOT NULL DEFAULT '',
    platform        TEXT NOT NULL DEFAULT '',
    app_version     TEXT NOT NULL DEFAULT '',
    app_build       TEXT NOT NULL DEFAULT '',
    os_version      TEXT NOT NULL DEFAULT '',
    ip              TEXT NOT NULL DEFAULT '',
    request_id      TEXT NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL
);
CREATE INDEX idx_auth_events_created ON auth_events (created_at);
CREATE INDEX idx_auth_events_user ON auth_events (user_id, created_at);
CREATE INDEX idx_auth_events_name ON auth_events (event_name, created_at);
CREATE INDEX idx_auth_events_subject ON auth_events (subject_hash, created_at);

-- Сервер қайтарған қателер: тек метадерек (дене, тақырып, мәтін жоқ).
CREATE TABLE api_errors (
    id              TEXT PRIMARY KEY,
    request_id      TEXT NOT NULL DEFAULT '',
    trace_id        TEXT NOT NULL DEFAULT '',
    user_id         TEXT REFERENCES users (id) ON DELETE SET NULL,
    installation_id TEXT NOT NULL DEFAULT '',
    session_id      TEXT NOT NULL DEFAULT '',
    platform        TEXT NOT NULL DEFAULT '',
    app_version     TEXT NOT NULL DEFAULT '',
    app_build       TEXT NOT NULL DEFAULT '',
    os_version      TEXT NOT NULL DEFAULT '',
    method          TEXT NOT NULL,
    route           TEXT NOT NULL,                         -- route pattern, never a raw path with ids
    status_code     INTEGER NOT NULL,
    error_code      TEXT NOT NULL DEFAULT '',
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    occurred_at     INTEGER NOT NULL
);
CREATE INDEX idx_api_errors_occurred ON api_errors (occurred_at);
CREATE INDEX idx_api_errors_user ON api_errors (user_id, occurred_at);
CREATE INDEX idx_api_errors_version ON api_errors (platform, app_version, app_build);
CREATE INDEX idx_api_errors_request ON api_errors (request_id);

-- Әкімші аудиті: сұраныспен байланыстыру және сүзгілер үшін индекстер.
ALTER TABLE admin_audit_logs ADD COLUMN request_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_audit_action ON admin_audit_logs (action, created_at);
CREATE INDEX idx_audit_admin ON admin_audit_logs (admin_id, created_at);
CREATE INDEX idx_audit_entity ON admin_audit_logs (entity_type, entity_id);
