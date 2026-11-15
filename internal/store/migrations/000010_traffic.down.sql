ALTER TABLE subscriptions DROP COLUMN IF EXISTS traffic_used_bytes, DROP COLUMN IF EXISTS traffic_limit_bytes;
ALTER TABLE peers DROP COLUMN IF EXISTS sent_counter, DROP COLUMN IF EXISTS received_counter;
DROP TABLE IF EXISTS traffic_daily;
