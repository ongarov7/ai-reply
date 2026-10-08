-- 0012_plan_visibility: тарифтің қолданбада көрінуі және сатып алу ауыстырғышы.
--
-- plans.is_visible: whether customers see the plan (apps, landing, /api/v1/plans).
-- It is separate from is_active (the plan can be given to new users) and from
-- archived_at. Hiding a plan never touches subscriptions or payments: people
-- already on it keep their entitlement and still see it as their current plan.
--
-- First public release is free: paid plans start hidden, the free plan stays
-- visible. The admin can show them again from the Plans page.
--
-- purchases_enabled: the admin switch for buying plans. It only takes effect
-- when the server also has a verified billing integration (see payments), so
-- showing a plan or flipping this switch alone never starts taking payments.
--
-- Only additive changes. Rollback: older binaries ignore the column and the
-- setting, so deploying the previous build is enough.
ALTER TABLE plans ADD COLUMN is_visible INTEGER NOT NULL DEFAULT 1;
UPDATE plans SET is_visible = 0 WHERE is_free = 0;

INSERT OR IGNORE INTO system_settings (key, value, updated_at)
VALUES ('purchases_enabled', 'false', 0);
