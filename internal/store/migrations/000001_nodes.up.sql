CREATE TABLE IF NOT EXISTS nodes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    ip inet NOT NULL UNIQUE,
    ssh_port integer NOT NULL CHECK (ssh_port BETWEEN 1 AND 65535),
    vpn_port integer NOT NULL CHECK (vpn_port BETWEEN 1 AND 65535),
    host_key text NOT NULL,
    agent_token_hash bytea NOT NULL,
    status text NOT NULL DEFAULT 'installing' CHECK (status IN ('installing', 'ready', 'failed')),
    enabled boolean NOT NULL DEFAULT true,
    public_key text,
    header_key bytea,
    last_seen_at timestamptz,
    handshakes_reported_at timestamptz,
    connection_ok boolean,
    connection_checked_at timestamptz,
    revision bigint NOT NULL DEFAULT 1,
    applied_revision bigint NOT NULL DEFAULT 0,
    restart_revision bigint NOT NULL DEFAULT 0,
    restarted_revision bigint NOT NULL DEFAULT 0,
    error text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS nodes_created_at_id_idx ON nodes(created_at, id);
CREATE INDEX IF NOT EXISTS nodes_ready_id_idx ON nodes(id) WHERE status = 'ready';
