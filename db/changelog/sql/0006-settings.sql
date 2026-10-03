--liquibase formatted sql

--changeset blinks:0006-settings
--comment: Key/value shop settings: hours, WhatsApp number, low-stock default, templates, weekly summary toggle.
CREATE TABLE settings (
  key        text        PRIMARY KEY CHECK (key ~ '^[a-z][a-z0-9_.]*$'),
  value      jsonb       NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER settings_set_updated_at BEFORE UPDATE ON settings
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
--rollback DROP TABLE settings;
