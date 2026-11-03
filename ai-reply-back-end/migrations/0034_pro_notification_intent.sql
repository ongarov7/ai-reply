-- Existing history, especially shadow evaluations, never requests delivery.
-- New enforced denial events can record notification intent in their own
-- audit transaction. A worker recovers intent after request-time queue outages.
ALTER TABLE pro_device_events ADD COLUMN notify_requested INTEGER NOT NULL DEFAULT 0 CHECK (notify_requested IN (0,1));
ALTER TABLE pro_device_events ADD COLUMN notification_processed_at INTEGER;
CREATE INDEX idx_pro_device_notification_intent ON pro_device_events(created_at,id)
    WHERE notify_requested=1 AND notification_processed_at IS NULL;
