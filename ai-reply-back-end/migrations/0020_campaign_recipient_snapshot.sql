-- Freeze recipients atomically when an administrator queues a campaign.
-- Existing queued campaigns retain NULL and are snapshotted once by the worker.
ALTER TABLE notification_campaigns ADD COLUMN recipient_snapshot_at INTEGER;
