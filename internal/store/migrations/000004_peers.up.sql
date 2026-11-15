CREATE TABLE IF NOT EXISTS peers (
    subscription_id uuid NOT NULL REFERENCES subscriptions(id),
    node_id uuid NOT NULL REFERENCES nodes(id),
    address integer NOT NULL CHECK (address BETWEEN 2 AND 65534),
    private_key bytea NOT NULL,
    public_key text NOT NULL UNIQUE,
    last_handshake_at timestamptz,
    current_handshake_at timestamptz,
    PRIMARY KEY (subscription_id, node_id),
    UNIQUE (node_id, address)
);
