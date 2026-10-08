-- 0013_ai_reports: AI жауабына шағымдар (POST /api/v1/ai/reports).
--
-- A person can report a generated reply or message from the app or the
-- keyboard. Only what they send is stored: the reason, an optional comment
-- and — only when they leave "send the reply text" on — the generated text
-- itself. Never the copied message or the instruction. Administrators read
-- the reports in the admin panel and mark them resolved.
--
-- Rows go away with the account (ON DELETE CASCADE).
--
-- Only additive. Rollback: older binaries do not know the table and leave it alone.
CREATE TABLE ai_reports (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    mode        TEXT NOT NULL,                     -- reply | compose
    reason      TEXT NOT NULL,                     -- offensive | harmful | false_info | wrong_language | other
    comment     TEXT NOT NULL DEFAULT '',          -- ≤ 500 characters
    text        TEXT NOT NULL DEFAULT '',          -- the generated text, only if the person chose to send it; ≤ 2000
    platform    TEXT NOT NULL DEFAULT '',          -- ios | android
    app_version TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'open',      -- open | resolved
    created_at  INTEGER NOT NULL,
    resolved_at INTEGER,
    resolved_by TEXT NOT NULL DEFAULT ''           -- admin_users.id
);
CREATE INDEX idx_ai_reports_status ON ai_reports (status, created_at);
CREATE INDEX idx_ai_reports_user ON ai_reports (user_id);
