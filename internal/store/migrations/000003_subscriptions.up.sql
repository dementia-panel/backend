CREATE TABLE IF NOT EXISTS subscriptions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id),
    name text NOT NULL,
    token_hash bytea NOT NULL UNIQUE,
    enabled boolean NOT NULL DEFAULT true,
    expires_at timestamptz NOT NULL,
    removed_peer_last_handshake_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS subscriptions_user_id_idx ON subscriptions(user_id);
CREATE INDEX IF NOT EXISTS subscriptions_created_at_id_idx ON subscriptions(created_at, id);
