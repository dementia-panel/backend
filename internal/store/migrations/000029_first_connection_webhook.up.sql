CREATE TABLE IF NOT EXISTS user_first_connections (
    user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    connected_at timestamptz NOT NULL
);

INSERT INTO user_first_connections (user_id, connected_at)
SELECT user_id, min(connected_at)
FROM (
    SELECT s.user_id, p.last_handshake_at AS connected_at
    FROM peers p
    JOIN subscriptions s ON s.id = p.subscription_id
    WHERE p.last_handshake_at IS NOT NULL
    UNION ALL
    SELECT user_id, removed_peer_last_handshake_at
    FROM subscriptions
    WHERE removed_peer_last_handshake_at IS NOT NULL
) history
GROUP BY user_id
ON CONFLICT (user_id) DO NOTHING;

CREATE OR REPLACE FUNCTION emit_first_connection_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    connected_user uuid;
    inserted_user uuid;
BEGIN
    SELECT user_id INTO connected_user FROM subscriptions WHERE id = NEW.subscription_id;
    IF connected_user IS NULL THEN RETURN NEW; END IF;

    INSERT INTO user_first_connections (user_id, connected_at)
    VALUES (connected_user, NEW.last_handshake_at)
    ON CONFLICT (user_id) DO NOTHING
    RETURNING user_id INTO inserted_user;

    IF inserted_user IS NOT NULL AND current_setting('dementia.webhook_enabled', true) = 'on' THEN
        INSERT INTO events (scope, event, data) VALUES (
            'user', 'user.first_connected',
            jsonb_build_object('id', connected_user, 'device_id', NEW.device_id, 'connected_at', NEW.last_handshake_at)
        );
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS first_connection_events ON peers;
CREATE TRIGGER first_connection_events AFTER UPDATE OF last_handshake_at ON peers
    FOR EACH ROW WHEN (OLD.last_handshake_at IS NULL AND NEW.last_handshake_at IS NOT NULL)
    EXECUTE FUNCTION emit_first_connection_event();
