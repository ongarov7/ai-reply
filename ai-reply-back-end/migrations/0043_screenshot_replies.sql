-- 0043_screenshot_replies: replies written from a screenshot of a chat.
--
-- A screenshot reply is priced by the tokens it really used, in standard
-- replies (internal/screenshot, docs/SCREENSHOT_REPLIES.md). Before the call
-- the server reserves the most it can cost (one 'screenshot' reservation with
-- the Idempotency-Key plus 'screenshot_extra' rows without one); afterwards it
-- commits what the provider reported and refunds the rest. The usage event
-- keeps the numbers that explain the charge: cached input and reasoning
-- tokens, the maximum the person approved and the policy that priced it.
--
-- PRIVACY: numbers only. No screenshot, recognised text, instruction or reply
-- is stored anywhere.
--
-- Only additive. Rollback: older binaries ignore the columns.
ALTER TABLE ai_usage_events ADD COLUMN cached_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE ai_usage_events ADD COLUMN reasoning_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE ai_usage_events ADD COLUMN max_units INTEGER NOT NULL DEFAULT 0;
ALTER TABLE ai_usage_events ADD COLUMN credit_policy TEXT NOT NULL DEFAULT '';
