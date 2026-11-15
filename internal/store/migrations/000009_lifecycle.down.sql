DROP INDEX IF EXISTS nodes_retired_idx;
ALTER TABLE nodes DROP COLUMN IF EXISTS retired_at;
