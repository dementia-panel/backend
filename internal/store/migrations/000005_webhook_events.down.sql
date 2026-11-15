DO $$
BEGIN
    IF to_regclass('subscriptions') IS NOT NULL THEN
        DROP TRIGGER IF EXISTS subscription_events ON subscriptions;
    END IF;
    IF to_regclass('nodes') IS NOT NULL THEN
        DROP TRIGGER IF EXISTS node_events ON nodes;
    END IF;
    IF to_regclass('users') IS NOT NULL THEN
        DROP TRIGGER IF EXISTS user_events ON users;
    END IF;
END;
$$;
DROP FUNCTION IF EXISTS record_entity_event();
DROP TABLE IF EXISTS events;
