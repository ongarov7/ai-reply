-- Released clients keep a per-account session device ID. Returning to an
-- account can create another ID on the same registered installation. Preserve
-- the historical device and its slot; only an authenticated registration may
-- add an immutable, same-account session alias. No existing owner is selected.
CREATE TABLE pro_device_session_aliases (
    device_id           TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
    user_id             TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    canonical_device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    created_at          INTEGER NOT NULL,
    revoked_at          INTEGER,
    revoked_reason      TEXT NOT NULL DEFAULT '',
    CHECK (device_id <> canonical_device_id)
);
CREATE INDEX idx_pro_device_session_alias_owner
    ON pro_device_session_aliases(user_id,canonical_device_id);

CREATE TRIGGER pro_device_session_alias_identity_immutable
BEFORE UPDATE OF device_id,user_id,canonical_device_id ON pro_device_session_aliases
BEGIN
    SELECT RAISE(ABORT,'pro device session alias identity is immutable');
END;
