DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM peers GROUP BY subscription_id,node_id HAVING count(*)>1) THEN
        RAISE EXCEPTION 'Remove additional devices before rolling back device support';
    END IF;
END $$;
ALTER TABLE peers DROP CONSTRAINT IF EXISTS peers_pkey;
ALTER TABLE peers ADD PRIMARY KEY (subscription_id,node_id);
ALTER TABLE peers DROP COLUMN IF EXISTS device_id;
DROP TABLE IF EXISTS subscription_nodes;
DROP TRIGGER IF EXISTS user_device_events ON user_devices;
DROP FUNCTION IF EXISTS emit_user_device_event();
DROP TABLE IF EXISTS user_devices;
DROP INDEX IF EXISTS users_telegram_id_lookup_idx;
ALTER TABLE users DROP COLUMN IF EXISTS devices_initialized;
ALTER TABLE users DROP COLUMN IF EXISTS device_limit;
DROP TABLE IF EXISTS device_settings;
