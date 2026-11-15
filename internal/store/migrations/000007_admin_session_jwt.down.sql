DROP INDEX IF EXISTS admin_sessions_session_id_idx;
ALTER TABLE admin_sessions DROP COLUMN IF EXISTS session_id;
