BEGIN;
ALTER TABLE users DROP COLUMN IF EXISTS subscription_plan_id;
ALTER TABLE subscriptions DROP COLUMN IF EXISTS plan_id;
UPDATE subscriptions SET expires_at='9999-12-31 23:59:59+00' WHERE expires_at IS NULL;
ALTER TABLE subscriptions ALTER COLUMN expires_at SET NOT NULL;
DROP TABLE IF EXISTS subscription_plan_nodes;
DROP TABLE IF EXISTS subscription_plans;
COMMIT;
