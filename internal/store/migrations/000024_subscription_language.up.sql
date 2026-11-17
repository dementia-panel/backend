ALTER TABLE subscription_branding ADD COLUMN IF NOT EXISTS default_language text NOT NULL DEFAULT 'en' CHECK (default_language IN ('en','ru'));
