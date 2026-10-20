-- 0016_permission_diagnostics: пернетақта мен рұқсаттар диагностикасы, еске салулар журналы.
--
-- installation_diagnostics holds what the app last observed on one
-- installation (PUT /api/v1/installations/{id}/diagnostics): enum statuses
-- and timestamps only, never clipboard content. Ownership comes from the join
-- to app_installations.user_id; the row goes with the installation (ON DELETE
-- CASCADE) and is deleted with the account. *_verified_at are sticky: once
-- verified, a later ambiguous check never restarts reminders.
--
-- notification_reminders is the cooldown ledger of the reminder pushes
-- (onboarding_incomplete, keyboard_setup_incomplete, clipboard_help, …): one
-- row per account and type with the send count, the last send and the
-- resolution. Dedupe rows in notifications are deleted by retention, so "at
-- most N, ever" needs its own table. last_source: auto | admin.
--
-- notification_campaigns.notification_type lets an admin reminder campaign
-- carry the reminder type into notifications.type (delivery history filter);
-- ordinary campaigns stay 'campaign'. notifications.not_before is when a push
-- deferred out of quiet hours may first go out: its TTL counts from there.
-- The product_events index serves per-user lookups.
--
-- Only additive. Rollback: older binaries ignore the new tables and columns;
-- drop them by hand afterwards if needed (DROP TABLE installation_diagnostics,
-- notification_reminders; ALTER TABLE … DROP COLUMN; DROP INDEX
-- idx_product_events_user).
CREATE TABLE installation_diagnostics (
    installation_id          TEXT PRIMARY KEY REFERENCES app_installations (id) ON DELETE CASCADE,
    keyboard_setup_status    TEXT NOT NULL DEFAULT 'unknown',
    keyboard_selected_status TEXT NOT NULL DEFAULT 'unknown',
    full_access_status       TEXT NOT NULL DEFAULT 'unknown',
    clipboard_status         TEXT NOT NULL DEFAULT 'unknown',
    onboarding_status        TEXT NOT NULL DEFAULT 'unknown',
    status_source            TEXT NOT NULL DEFAULT 'unknown',
    last_successful_paste_at INTEGER,
    last_keyboard_check_at   INTEGER,
    keyboard_verified_at     INTEGER,
    full_access_verified_at  INTEGER,
    clipboard_denied_at      INTEGER,
    onboarding_completed_at  INTEGER,
    reported_at              INTEGER NOT NULL,
    created_at               INTEGER NOT NULL,
    updated_at               INTEGER NOT NULL
);
CREATE INDEX idx_diagnostics_keyboard ON installation_diagnostics (keyboard_setup_status, updated_at);
CREATE INDEX idx_diagnostics_onboarding ON installation_diagnostics (onboarding_status, updated_at);

CREATE TABLE notification_reminders (
    user_id              TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type                 TEXT NOT NULL,
    send_count           INTEGER NOT NULL DEFAULT 0,
    last_sent_at         INTEGER,
    last_notification_id TEXT NOT NULL DEFAULT '',
    last_source          TEXT NOT NULL DEFAULT '',
    resolved_at          INTEGER,
    updated_at           INTEGER NOT NULL,
    PRIMARY KEY (user_id, type)
);

ALTER TABLE notification_campaigns ADD COLUMN notification_type TEXT NOT NULL DEFAULT 'campaign';
ALTER TABLE notifications ADD COLUMN not_before INTEGER;

CREATE INDEX idx_product_events_user ON product_events (user_id, name, created_at);
