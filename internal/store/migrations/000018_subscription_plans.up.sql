BEGIN;
CREATE TABLE IF NOT EXISTS subscription_plans (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS subscription_plan_nodes (
    plan_id uuid NOT NULL REFERENCES subscription_plans(id) ON DELETE CASCADE,
    node_id uuid NOT NULL REFERENCES nodes(id),
    PRIMARY KEY (plan_id, node_id)
);
CREATE INDEX IF NOT EXISTS subscription_plan_nodes_node_idx ON subscription_plan_nodes (node_id);
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS plan_id uuid REFERENCES subscription_plans(id);
CREATE UNIQUE INDEX IF NOT EXISTS subscriptions_user_plan_idx ON subscriptions (user_id, plan_id) WHERE plan_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS subscriptions_plan_idx ON subscriptions (plan_id);
ALTER TABLE users ADD COLUMN IF NOT EXISTS subscription_plan_id uuid REFERENCES subscription_plans(id);
CREATE INDEX IF NOT EXISTS users_subscription_plan_idx ON users (subscription_plan_id);
ALTER TABLE subscriptions ALTER COLUMN expires_at DROP NOT NULL;

INSERT INTO subscription_plans (id, name, created_at)
SELECT id, name, created_at FROM subscriptions;
INSERT INTO subscription_plan_nodes (plan_id, node_id)
SELECT p.subscription_id, p.node_id FROM peers p JOIN nodes n ON n.id=p.node_id WHERE n.retired_at IS NULL;
UPDATE subscriptions SET plan_id=id, enabled=enabled AND expires_at>now(), expires_at=NULL;
UPDATE users u SET subscription_plan_id=(
    SELECT s.plan_id FROM subscriptions s WHERE s.user_id=u.id AND s.enabled ORDER BY s.created_at DESC,s.id DESC LIMIT 1
);
UPDATE nodes SET revision=revision+1 WHERE id IN (SELECT DISTINCT node_id FROM peers);
COMMIT;
