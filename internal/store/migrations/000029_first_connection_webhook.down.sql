DROP TRIGGER IF EXISTS first_connection_events ON peers;
DROP FUNCTION IF EXISTS emit_first_connection_event();
DROP TABLE IF EXISTS user_first_connections;
