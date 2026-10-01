-- 0008_delivery_indexes: жеткізу есептері мен тізімдеріне арналған индекстер.
--
-- Campaign statistics, the campaign's delivery page and the operations
-- dashboard read notification_deliveries, the largest notification table.
-- These covering indexes let SQLite answer them from the index alone; the
-- two older indexes they replace are their prefixes.

CREATE INDEX idx_deliveries_campaign_stats
    ON notification_deliveries (campaign_id, status, platform, opened_at);
CREATE INDEX idx_deliveries_campaign_created
    ON notification_deliveries (campaign_id, created_at DESC, id);
CREATE INDEX idx_deliveries_created_stats
    ON notification_deliveries (created_at, status, platform, opened_at);

DROP INDEX idx_deliveries_campaign;
DROP INDEX idx_deliveries_created;
