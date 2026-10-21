-- 0019_welcome_bonus_20: сәлемдесу бонусы — 20 жауап.
--
-- The owner lowered the welcome bonus for new accounts from 50 to 20 replies.
-- This runs once: only a value still at the old 50 changes, anything an
-- administrator already chose stays. From here on the admin panel (Settings ›
-- Welcome bonus) is the only place that sets it. Accounts that already received
-- their bonus keep it, and welcome_bonus_since does not move.
--
-- Rollback: set the value back in the admin panel.
UPDATE system_settings
SET value = '20', updated_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE key = 'welcome_bonus_units' AND value = '50';
