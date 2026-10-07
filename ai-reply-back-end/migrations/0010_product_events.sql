-- 0010_product_events: қолданбалардың өнім оқиғалары (онбординг қадамдары,
-- пернетақта қосылғаны, баптау ауыстырғыштары) — тек санау үшін.
--
-- Only additive: a new table, nothing existing changes. A row holds an event
-- name and its properties from the server's allowlist (enum codes, small
-- integers, booleans), so no text the user typed can ever land here.
-- client_ts is the device's own clock (NULL when it sent none).
--
-- Number 0010 and the table name on purpose: the unmerged notification branch
-- owns 0006–0008 and its own analytics tables.
-- Rollback: older binaries never touch the table, so deploying the previous
-- build is enough; DROP TABLE product_events removes it by hand if needed.
CREATE TABLE product_events (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    props       TEXT NOT NULL DEFAULT '{}',          -- JSON: тек рұқсат етілген кілттер мен мәндер
    platform    TEXT NOT NULL DEFAULT '',            -- ios | android
    app_version TEXT NOT NULL DEFAULT '',
    client_ts   INTEGER,
    created_at  INTEGER NOT NULL
);
CREATE INDEX idx_product_events_name ON product_events (name, created_at);
