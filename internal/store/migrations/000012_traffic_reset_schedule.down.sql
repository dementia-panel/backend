DROP INDEX IF EXISTS subscriptions_traffic_reset_at_idx;
ALTER TABLE subscriptions DROP COLUMN IF EXISTS traffic_reset_at, DROP COLUMN IF EXISTS traffic_reset_days;
