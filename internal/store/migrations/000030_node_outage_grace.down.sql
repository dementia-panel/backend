DROP TRIGGER IF EXISTS reset_node_connection_alert ON nodes;
DROP FUNCTION IF EXISTS reset_node_connection_alert();
ALTER TABLE nodes DROP COLUMN IF EXISTS connection_alerted;
ALTER TABLE nodes DROP COLUMN IF EXISTS connection_lost_at;
