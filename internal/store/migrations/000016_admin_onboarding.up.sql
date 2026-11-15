ALTER TABLE administrators ADD COLUMN IF NOT EXISTS password_change_required boolean NOT NULL DEFAULT false;
