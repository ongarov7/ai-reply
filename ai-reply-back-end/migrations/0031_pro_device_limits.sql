-- Additive Pro device authorization foundation. No historical installation is
-- selected, replaced or revoked by this migration. Existing clients and older
-- binaries can ignore these tables. Roll back by disabling the feature and
-- retaining this schema; do not delete security evidence to roll back a binary.
CREATE TABLE pro_device_authorizations (
    id                  TEXT PRIMARY KEY,
    user_id             TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id           TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    category            TEXT NOT NULL CHECK (category IN ('mobile','desktop')),
    platform            TEXT NOT NULL CHECK (platform IN ('ios','android','ipados','macos','windows')),
    display_name        TEXT NOT NULL DEFAULT '',
    first_authorized_at INTEGER NOT NULL,
    last_seen_at        INTEGER NOT NULL,
    revoked_at          INTEGER,
    revoked_reason      TEXT NOT NULL DEFAULT '',
    UNIQUE (user_id,device_id),
    CHECK ((category = 'mobile' AND platform IN ('ios','android','ipados')) OR
           (category = 'desktop' AND platform IN ('macos','windows')))
);
-- A database constraint, rather than an application pre-check, owns each slot.
CREATE UNIQUE INDEX idx_pro_device_active_slot
    ON pro_device_authorizations(user_id,category) WHERE revoked_at IS NULL;
CREATE INDEX idx_pro_device_user_history ON pro_device_authorizations(user_id,first_authorized_at);

CREATE TABLE pro_device_events (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id  TEXT REFERENCES devices(id) ON DELETE SET NULL,
    category   TEXT NOT NULL DEFAULT '',
    platform   TEXT NOT NULL DEFAULT '',
    kind       TEXT NOT NULL,
    mode       TEXT NOT NULL DEFAULT '',
    reason     TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_pro_device_events_user ON pro_device_events(user_id,created_at DESC);
CREATE INDEX idx_pro_device_events_kind ON pro_device_events(kind,created_at);

-- The existing push registry has a separate installation ID. Only explicit,
-- authenticated registrations create this link; no model/IP/push-token guesses.
CREATE TABLE pro_device_installations (
    device_id       TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    installation_id TEXT NOT NULL REFERENCES app_installations(id) ON DELETE CASCADE,
    UNIQUE (user_id,installation_id)
);

-- Shadow evidence has no winner and cannot be promoted into authorization
-- without an explicit, reviewed migration decision.
CREATE TABLE pro_device_observations (
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id     TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    category      TEXT NOT NULL CHECK (category IN ('mobile','desktop')),
    platform      TEXT NOT NULL,
    first_seen_at INTEGER NOT NULL,
    last_seen_at  INTEGER NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (user_id,device_id)
);
CREATE INDEX idx_pro_device_observations_category ON pro_device_observations(user_id,category);
