CREATE TABLE IF NOT EXISTS device_subscription_links (
    subscription_id uuid NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    device_id uuid NOT NULL REFERENCES user_devices(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    token_encrypted bytea NOT NULL,
    subscription_token_hash bytea NOT NULL,
    PRIMARY KEY (subscription_id, device_id)
);
CREATE INDEX IF NOT EXISTS device_subscription_links_device_idx ON device_subscription_links(device_id);
