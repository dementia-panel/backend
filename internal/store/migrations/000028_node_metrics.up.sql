CREATE TABLE IF NOT EXISTS node_online_peak (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    users_count integer NOT NULL DEFAULT 0 CHECK (users_count >= 0)
);
CREATE INDEX IF NOT EXISTS traffic_daily_day_idx ON traffic_daily(day);
CREATE INDEX IF NOT EXISTS peers_current_handshake_idx ON peers(current_handshake_at) WHERE current_handshake_at IS NOT NULL;
