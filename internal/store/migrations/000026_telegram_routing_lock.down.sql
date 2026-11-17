CREATE OR REPLACE FUNCTION route_telegram_notification() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF current_setting('dementia.telegram_enabled',true)='on' AND EXISTS(
  SELECT 1 FROM telegram_settings WHERE id AND categories->>NEW.scope='true'
 ) THEN INSERT INTO telegram_notifications(event,data) VALUES(NEW.event,NEW.data); END IF;
 IF current_setting('dementia.webhook_enabled',true)='on' THEN RETURN NEW; END IF;
 RETURN NULL;
END; $$;
