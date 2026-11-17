-- 0042_ai_context: the account's AI context, edited in the website cabinet.
--
-- One JSON document per account: roles, occupation, communication style,
-- about, audience and a note for each built-in recipient (friend, client,
-- colleague, partner); see docs/web-cabinet-ai-context.md. The server adds it
-- to every reply and compose request as data. revision is the optimistic
-- concurrency counter: a save names the revision it edited, so two devices
-- cannot silently overwrite each other.
--
-- PRIVACY: the document is what the user typed about themselves. It is never
-- logged, it is sent to the AI provider only inside a generation, and it goes
-- away with the account (ON DELETE CASCADE).
--
-- ai_usage_events gains the units a generation charged (the context surcharge
-- makes a reply cost 1-3) and the number of context characters it carried,
-- so the admin panel can see what the surcharge does. Numbers only.
--
-- Only additive. Rollback: older binaries ignore the table and the columns.
CREATE TABLE ai_contexts (
    user_id    TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    document   TEXT NOT NULL DEFAULT '{}',
    revision   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

ALTER TABLE ai_usage_events ADD COLUMN units INTEGER NOT NULL DEFAULT 1;
ALTER TABLE ai_usage_events ADD COLUMN context_chars INTEGER NOT NULL DEFAULT 0;
