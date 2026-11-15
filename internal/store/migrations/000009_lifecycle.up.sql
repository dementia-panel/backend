ALTER TABLE nodes ADD COLUMN IF NOT EXISTS retired_at timestamptz;
CREATE INDEX IF NOT EXISTS nodes_retired_idx ON nodes(retired_at) WHERE retired_at IS NOT NULL;
