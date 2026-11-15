ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS traffic_reset_days integer CHECK (traffic_reset_days BETWEEN 1 AND 366);
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS traffic_reset_at timestamptz;
CREATE INDEX IF NOT EXISTS subscriptions_traffic_reset_at_idx ON subscriptions(traffic_reset_at)
    WHERE traffic_reset_at IS NOT NULL;
