ALTER TABLE nodes ADD COLUMN IF NOT EXISTS connection_lost_at timestamptz;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS connection_alerted boolean NOT NULL DEFAULT false;
CREATE OR REPLACE FUNCTION reset_node_connection_alert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.connection_lost_at := NULL;
    NEW.connection_alerted := false;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS reset_node_connection_alert ON nodes;
CREATE TRIGGER reset_node_connection_alert BEFORE UPDATE OF status, agent_token_hash ON nodes
    FOR EACH ROW WHEN (NEW.status = 'installing' AND
        (OLD.status IS DISTINCT FROM NEW.status OR OLD.agent_token_hash IS DISTINCT FROM NEW.agent_token_hash))
    EXECUTE FUNCTION reset_node_connection_alert();
DELETE FROM telegram_notifications WHERE event IN ('node.connection_lost', 'node.connection_restored');
