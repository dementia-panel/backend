ALTER TABLE nodes ADD COLUMN IF NOT EXISTS endpoint_host text;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS endpoint_port integer CHECK (endpoint_port BETWEEN 1 AND 65535);
