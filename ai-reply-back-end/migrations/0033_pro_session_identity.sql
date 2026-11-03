-- Optional installation binding; old sessions retain an empty identity and
-- remain compatible while enforcement is off. Refresh rotation preserves it.
ALTER TABLE refresh_tokens ADD COLUMN installation_identity TEXT NOT NULL DEFAULT '';
