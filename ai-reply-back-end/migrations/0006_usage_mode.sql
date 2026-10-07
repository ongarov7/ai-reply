-- 0006_usage_mode: AI оқиғасы қай режимнен келгенін сақтайды.
--
-- reply   — көшірілген хабарламаға жауап (бұрынғы жалғыз режим);
-- compose — пайдаланушы нұсқауы бойынша жаңа хабарлама жазу.
--
-- Only additive: existing rows and older binaries get 'reply', which is what
-- every event before this migration was. Quotas are unchanged — both modes
-- spend the same daily and monthly counters.
ALTER TABLE ai_usage_events ADD COLUMN mode TEXT NOT NULL DEFAULT 'reply';
