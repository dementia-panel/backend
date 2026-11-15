CREATE OR REPLACE FUNCTION route_telegram_notification() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.scope = 'node' AND current_setting('relay.telegram_enabled', true) = 'on' THEN
        INSERT INTO telegram_notifications(event, data) VALUES (NEW.event, NEW.data);
    END IF;
    IF current_setting('relay.webhook_enabled', true) = 'on' THEN
        RETURN NEW;
    END IF;
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION record_entity_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    payload jsonb;
    event_name text;
    event_scope text;
BEGIN
    IF current_setting('relay.webhook_enabled', true) IS DISTINCT FROM 'on'
       AND (TG_TABLE_NAME <> 'nodes' OR current_setting('relay.telegram_enabled', true) IS DISTINCT FROM 'on') THEN
        RETURN NEW;
    END IF;
    IF TG_TABLE_NAME = 'users' THEN
        event_scope := 'user';
        payload := jsonb_build_object('id', NEW.id, 'username', NEW.username,
            'telegram_id', NEW.telegram_id, 'enabled', NEW.enabled, 'updated_at', NEW.updated_at);
        IF TG_OP = 'INSERT' THEN
            event_name := 'user.created';
        ELSE
            IF OLD.username IS DISTINCT FROM NEW.username OR OLD.telegram_id IS DISTINCT FROM NEW.telegram_id THEN
                INSERT INTO events(scope, event, data) VALUES (event_scope, 'user.modified', payload);
            END IF;
            IF OLD.enabled IS DISTINCT FROM NEW.enabled THEN
                event_name := CASE WHEN NEW.enabled THEN 'user.enabled' ELSE 'user.disabled' END;
            END IF;
        END IF;
    ELSIF TG_TABLE_NAME = 'nodes' THEN
        event_scope := 'node';
        payload := jsonb_build_object('id', NEW.id, 'name', NEW.name, 'ip', host(NEW.ip),
            'enabled', NEW.enabled, 'status', NEW.status, 'error', NEW.error);
        IF TG_OP = 'INSERT' THEN
            event_name := 'node.created';
        ELSE
            IF OLD.name IS DISTINCT FROM NEW.name THEN
                INSERT INTO events(scope, event, data) VALUES (event_scope, 'node.modified', payload);
            END IF;
            IF OLD.enabled IS DISTINCT FROM NEW.enabled THEN
                INSERT INTO events(scope, event, data) VALUES
                    (event_scope, CASE WHEN NEW.enabled THEN 'node.enabled' ELSE 'node.disabled' END, payload);
            END IF;
            IF OLD.restart_revision <> NEW.restart_revision THEN
                INSERT INTO events(scope, event, data) VALUES (event_scope, 'node.restart_requested', payload);
            END IF;
            IF OLD.restarted_revision <> NEW.restarted_revision THEN
                INSERT INTO events(scope, event, data) VALUES (event_scope, 'node.restarted', payload);
            END IF;
            IF OLD.status <> NEW.status THEN
                event_name := CASE NEW.status
                    WHEN 'ready' THEN 'node.installation_completed'
                    WHEN 'failed' THEN 'node.installation_failed'
                    ELSE 'node.installation_started' END;
            END IF;
        END IF;
    ELSE
        event_scope := 'subscription';
        payload := jsonb_build_object('id', NEW.id, 'user_id', NEW.user_id,
            'name', NEW.name, 'enabled', NEW.enabled, 'expires_at', NEW.expires_at);
        IF TG_OP = 'INSERT' THEN
            event_name := 'subscription.created';
        ELSE
            IF OLD.token_hash IS DISTINCT FROM NEW.token_hash THEN
                INSERT INTO events(scope, event, data) VALUES (event_scope, 'subscription.link_rotated', payload);
            END IF;
            IF OLD.expires_at IS DISTINCT FROM NEW.expires_at THEN
                INSERT INTO events(scope, event, data) VALUES (event_scope, 'subscription.modified', payload);
            END IF;
            IF OLD.enabled IS DISTINCT FROM NEW.enabled THEN
                event_name := CASE WHEN NEW.enabled THEN 'subscription.enabled' ELSE 'subscription.disabled' END;
            END IF;
        END IF;
    END IF;
    IF event_name IS NOT NULL THEN
        INSERT INTO events(scope, event, data) VALUES (event_scope, event_name, payload);
    END IF;
    RETURN NEW;
END;
$$;
