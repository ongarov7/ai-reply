-- Additive durable security-notice queue. Roll back the binary/configuration;
-- leave this table in place so queued notices and cooldown evidence survive.
CREATE TABLE pro_device_notification_jobs (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    device_category TEXT NOT NULL CHECK (device_category IN ('mobile','desktop')),
    platform TEXT NOT NULL,
    occurred_at INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','queued','failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at INTEGER NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    notification_id TEXT,
    updated_at INTEGER NOT NULL
);
CREATE INDEX idx_pro_device_notification_due ON pro_device_notification_jobs(status,next_attempt_at);
CREATE INDEX idx_pro_device_notification_cooldown ON pro_device_notification_jobs(user_id,occurred_at);
CREATE INDEX idx_pro_device_notification_device ON pro_device_notification_jobs(user_id,device_id,device_category,occurred_at);
