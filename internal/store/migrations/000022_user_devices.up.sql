CREATE TABLE IF NOT EXISTS device_settings (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    default_device_limit integer CHECK (default_device_limit BETWEEN 1 AND 10000)
);
INSERT INTO device_settings (id, default_device_limit) VALUES (true, 3) ON CONFLICT (id) DO NOTHING;
ALTER TABLE users ADD COLUMN IF NOT EXISTS device_limit integer CHECK (device_limit BETWEEN 1 AND 10000);
ALTER TABLE users ADD COLUMN IF NOT EXISTS devices_initialized boolean NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS users_telegram_id_lookup_idx ON users(telegram_id) WHERE telegram_id IS NOT NULL;
CREATE TABLE IF NOT EXISTS user_devices (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 128),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS user_devices_user_created_idx ON user_devices(user_id, created_at, id);
INSERT INTO user_devices (user_id, name, created_at)
SELECT u.id, 'Main device', u.created_at FROM users u
WHERE EXISTS (SELECT 1 FROM subscriptions s JOIN peers p ON p.subscription_id=s.id WHERE s.user_id=u.id)
AND NOT EXISTS (SELECT 1 FROM user_devices d WHERE d.user_id=u.id);
UPDATE users SET devices_initialized=true WHERE EXISTS (SELECT 1 FROM user_devices d WHERE d.user_id=users.id);
CREATE TABLE IF NOT EXISTS subscription_nodes (
    subscription_id uuid NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    node_id uuid NOT NULL REFERENCES nodes(id),
    PRIMARY KEY (subscription_id, node_id)
);
CREATE INDEX IF NOT EXISTS subscription_nodes_node_id_idx ON subscription_nodes(node_id);
INSERT INTO subscription_nodes SELECT DISTINCT subscription_id,node_id FROM peers ON CONFLICT DO NOTHING;
ALTER TABLE peers ADD COLUMN IF NOT EXISTS device_id uuid REFERENCES user_devices(id) ON DELETE CASCADE;
UPDATE peers p SET device_id=(SELECT d.id FROM user_devices d JOIN subscriptions s ON s.user_id=d.user_id
    WHERE s.id=p.subscription_id ORDER BY d.created_at,d.id LIMIT 1) WHERE device_id IS NULL;
ALTER TABLE peers ALTER COLUMN device_id SET NOT NULL;
ALTER TABLE peers DROP CONSTRAINT IF EXISTS peers_pkey;
ALTER TABLE peers ADD PRIMARY KEY (subscription_id,node_id,device_id);
CREATE INDEX IF NOT EXISTS peers_device_id_idx ON peers(device_id);

CREATE OR REPLACE FUNCTION emit_user_device_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE item user_devices; action text;
BEGIN
    IF TG_OP='DELETE' THEN item:=OLD; action:='deleted'; ELSE item:=NEW; action:=CASE WHEN TG_OP='INSERT' THEN 'created' ELSE 'updated' END; END IF;
    IF current_setting('dementia.webhook_enabled',true)='on' THEN
        INSERT INTO events(scope,event,data) VALUES('user','user.device_'||action,
            jsonb_build_object('id',item.id,'user_id',item.user_id,'name',item.name));
    END IF;
    RETURN item;
END $$;
DROP TRIGGER IF EXISTS user_device_events ON user_devices;
CREATE TRIGGER user_device_events AFTER INSERT OR UPDATE OF name OR DELETE ON user_devices FOR EACH ROW EXECUTE FUNCTION emit_user_device_event();
