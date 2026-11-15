CREATE TABLE IF NOT EXISTS administrators (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    username text NOT NULL,
    password_salt bytea NOT NULL,
    password_hash bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS admin_sessions (
    token_hash bytea PRIMARY KEY,
    administrator_id boolean NOT NULL REFERENCES administrators(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS admin_sessions_expires_at_idx ON admin_sessions(expires_at);
