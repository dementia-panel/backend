ALTER TABLE admin_sessions ADD COLUMN IF NOT EXISTS session_id uuid DEFAULT gen_random_uuid() NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS admin_sessions_session_id_idx ON admin_sessions(session_id);
