CREATE INDEX IF NOT EXISTS events_delivered_at_idx ON events(delivered_at)
    WHERE delivered_at IS NOT NULL;
