--liquibase formatted sql

--changeset blinks:0004-riders
--comment: People who deliver orders (BRIEF §9).
CREATE TABLE riders (
  id         uuid        PRIMARY KEY DEFAULT uuidv7(),
  name       text        NOT NULL CHECK (btrim(name) <> ''),
  phone      text        NOT NULL CHECK (phone ~ '^\+[1-9][0-9]{7,14}$'),
  kind       text        NOT NULL CHECK (kind IN ('OWN', 'BOLT_YANGO', 'COURIER')),
  is_active  boolean     NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER riders_set_updated_at BEFORE UPDATE ON riders
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
--rollback DROP TABLE riders;

--changeset blinks:0004-delivery-zones
--comment: Delivery zones and fees. TODO(open): real zones, fees and the free-delivery threshold come from the client (BRIEF §9).
CREATE TABLE delivery_zones (
  id                 uuid        PRIMARY KEY DEFAULT uuidv7(),
  name               text        NOT NULL CHECK (btrim(name) <> ''),
  method             text        NOT NULL CHECK (method IN ('RIDER', 'BUS_PARCEL')),
  fee_pesewas        bigint      NOT NULL CHECK (fee_pesewas >= 0),
  free_above_pesewas bigint      CHECK (free_above_pesewas > 0),
  is_active          boolean     NOT NULL DEFAULT true,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX delivery_zones_name_lower_key ON delivery_zones (lower(name));
CREATE TRIGGER delivery_zones_set_updated_at BEFORE UPDATE ON delivery_zones
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
--rollback DROP TABLE delivery_zones;
