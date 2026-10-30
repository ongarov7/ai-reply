-- Keep once-per-account promotion state after notification history expires.
-- Missing/disabled marketing preferences still do not imply consent.
ALTER TABLE notification_preferences ADD COLUMN web_topup_offer_queued_at INTEGER;
