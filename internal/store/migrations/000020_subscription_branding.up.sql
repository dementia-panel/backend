CREATE TABLE IF NOT EXISTS subscription_branding (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    name text NOT NULL,
    logo text NOT NULL DEFAULT ''
);
