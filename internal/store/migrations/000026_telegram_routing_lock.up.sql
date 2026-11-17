CREATE OR REPLACE FUNCTION route_telegram_notification() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE preferences jsonb;
BEGIN
 IF current_setting('dementia.telegram_enabled',true)='on' THEN
  SELECT categories INTO preferences FROM telegram_settings WHERE id=true FOR SHARE;
  IF preferences->>NEW.scope='true' THEN
   INSERT INTO telegram_notifications(event,data) VALUES(NEW.event,NEW.data);
  END IF;
 END IF;
 IF current_setting('dementia.webhook_enabled',true)='on' THEN RETURN NEW; END IF;
 RETURN NULL;
END; $$;
