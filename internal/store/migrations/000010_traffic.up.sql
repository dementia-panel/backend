CREATE TABLE IF NOT EXISTS traffic_daily (
    day date NOT NULL,
    node_id uuid NOT NULL REFERENCES nodes(id),
    user_id uuid NOT NULL,
    received_bytes bigint NOT NULL DEFAULT 0,
    sent_bytes bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (node_id, day, user_id)
);
CREATE INDEX IF NOT EXISTS traffic_daily_user_day_idx ON traffic_daily(user_id, day);
ALTER TABLE peers ADD COLUMN IF NOT EXISTS received_counter bigint NOT NULL DEFAULT 0;
ALTER TABLE peers ADD COLUMN IF NOT EXISTS sent_counter bigint NOT NULL DEFAULT 0;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS traffic_limit_bytes bigint CHECK (traffic_limit_bytes > 0);
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS traffic_used_bytes bigint NOT NULL DEFAULT 0;
