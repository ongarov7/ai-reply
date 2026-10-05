-- 0009_sender_profile: жауаптың орыс тіліндегі септеуіне керек жіберуші жынысы,
-- онбординг нұсқасы және әр AI оқиғасының промпт нұсқасы.
--
-- Only additive changes. Existing profiles get 'unspecified' (replies stay
-- gender-neutral until the user picks a value) and onboarding version 1 when
-- they had already finished the first onboarding. Existing usage events get
-- an empty prompt_version: they were written before prompts were versioned.
--
-- Number 0009 on purpose: the unmerged notification branch owns 0006–0008.
-- Rollback: older binaries ignore the new columns, so deploying the previous
-- build is enough. If the columns ever have to go, SQLite 3.35+ can drop them
-- by hand with ALTER TABLE … DROP COLUMN.
ALTER TABLE user_profiles ADD COLUMN grammatical_gender TEXT NOT NULL DEFAULT 'unspecified'; -- male | female | unspecified
ALTER TABLE user_profiles ADD COLUMN onboarding_version INTEGER NOT NULL DEFAULT 0;
UPDATE user_profiles SET onboarding_version = 1 WHERE onboarding_completed = 1;
ALTER TABLE ai_usage_events ADD COLUMN prompt_version TEXT NOT NULL DEFAULT '';
