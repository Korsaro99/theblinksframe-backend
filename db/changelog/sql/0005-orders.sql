--liquibase formatted sql

--changeset blinks:0005-customers
--comment: Guest customers, one per phone number. TODO(open): customer accounts (BRIEF §14 q11).
CREATE TABLE customers (
  id         uuid        PRIMARY KEY DEFAULT uuidv7(),
  name       text        NOT NULL CHECK (btrim(name) <> ''),
  phone      text        NOT NULL UNIQUE CHECK (phone ~ '^\+[1-9][0-9]{7,14}$'),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER customers_set_updated_at BEFORE UPDATE ON customers
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
--rollback DROP TABLE customers;

--changeset blinks:0005-orders
--comment: Orders (BRIEF §6, §7.1). id is public (used in /orders/{id}/pay); number is the display number "#1091".
CREATE TABLE orders (
  id                   uuid        PRIMARY KEY DEFAULT uuidv7(),
  number               bigint      NOT NULL UNIQUE GENERATED ALWAYS AS IDENTITY (START WITH 1001),
  source               text        NOT NULL CHECK (source IN ('WEBSITE', 'WHATSAPP', 'SHOP')),
  customer_id          uuid        NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,
  fulfilment           text        NOT NULL CHECK (fulfilment IN ('PICKUP', 'DELIVERY')),
  delivery_zone_id     uuid        REFERENCES delivery_zones (id) ON DELETE RESTRICT,
  area                 text,
  address_line         text,
  landmark             text,
  ghana_post_gps       text,
  subtotal_pesewas     bigint      NOT NULL CHECK (subtotal_pesewas >= 0),
  delivery_fee_pesewas bigint      NOT NULL DEFAULT 0 CHECK (delivery_fee_pesewas >= 0),
  discount_pesewas     bigint      NOT NULL DEFAULT 0 CHECK (discount_pesewas >= 0),
  total_pesewas        bigint      NOT NULL CHECK (total_pesewas >= 0),
  status               text        NOT NULL CHECK (status IN (
                         'AWAITING_PAYMENT', 'PAID', 'PACKED', 'OUT_FOR_DELIVERY',
                         'READY_FOR_PICKUP', 'DELIVERED', 'CANCELLED')),
  rider_id             uuid        REFERENCES riders (id) ON DELETE RESTRICT,
  private_note         text,
  -- Set when something needs the owner, e.g. an order was paid but stock had already run out.
  needs_attention      boolean     NOT NULL DEFAULT false,
  attention_reason     text,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  paid_at              timestamptz,
  delivered_at         timestamptz,
  cancelled_at         timestamptz,
  CONSTRAINT orders_total_adds_up CHECK (total_pesewas = subtotal_pesewas + delivery_fee_pesewas - discount_pesewas),
  -- Delivery needs a zone, area/town and landmark (BRIEF §9). Pickup at Gate 4 has no zone or address.
  CONSTRAINT orders_fulfilment_fields CHECK (
    (fulfilment = 'PICKUP' AND delivery_zone_id IS NULL AND delivery_fee_pesewas = 0)
    OR (fulfilment = 'DELIVERY' AND delivery_zone_id IS NOT NULL AND area IS NOT NULL AND landmark IS NOT NULL)
  ),
  CONSTRAINT orders_attention_has_reason CHECK (NOT needs_attention OR attention_reason IS NOT NULL),
  CONSTRAINT orders_cancelled_at_matches CHECK ((status = 'CANCELLED') = (cancelled_at IS NOT NULL))
);
CREATE INDEX orders_status_created_at_idx ON orders (status, created_at);
CREATE INDEX orders_paid_at_idx ON orders (paid_at) WHERE paid_at IS NOT NULL;
CREATE INDEX orders_customer_id_idx ON orders (customer_id);
CREATE INDEX orders_needs_attention_idx ON orders (created_at) WHERE needs_attention;
CREATE TRIGGER orders_set_updated_at BEFORE UPDATE ON orders
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
--rollback DROP TABLE orders;

--changeset blinks:0005-order-items
--comment: Lines of an order. Name, brand and prices are snapshots taken when the order is placed.
-- unit_price is what was charged; list_price is the catalogue price. They differ only when a price
-- was agreed in a WhatsApp or shop order (website orders always charge the catalogue price).
CREATE TABLE order_items (
  id                    uuid    PRIMARY KEY DEFAULT uuidv7(),
  order_id              uuid    NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
  product_id            uuid    NOT NULL REFERENCES products (id) ON DELETE RESTRICT,
  product_name_snapshot text    NOT NULL,
  brand_name_snapshot   text    NOT NULL,
  color_name            text,
  quantity              integer NOT NULL CHECK (quantity > 0),
  unit_price_pesewas    bigint  NOT NULL CHECK (unit_price_pesewas >= 0),
  list_price_pesewas    bigint  NOT NULL CHECK (list_price_pesewas > 0)
);
CREATE INDEX order_items_order_id_idx ON order_items (order_id);
CREATE INDEX order_items_product_id_idx ON order_items (product_id);
--rollback DROP TABLE order_items;

--changeset blinks:0005-order-events
--comment: One row per status change; this list is the tracking timeline.
CREATE TABLE order_events (
  id             uuid        PRIMARY KEY DEFAULT uuidv7(),
  order_id       uuid        NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
  from_status    text,
  to_status      text        NOT NULL,
  actor_staff_id uuid        REFERENCES staff_users (id) ON DELETE SET NULL,
  note           text,
  -- TODO(open): notification channel, WhatsApp or SMS or both (BRIEF §7.3).
  notified_via   text        NOT NULL DEFAULT 'NONE' CHECK (notified_via IN ('WHATSAPP', 'SMS', 'NONE')),
  created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX order_events_order_id_idx ON order_events (order_id, created_at);
--rollback DROP TABLE order_events;

--changeset blinks:0005-payments
--comment: Payment attempts. FAKE is the development provider. TODO(open): Paystack or Hubtel (BRIEF §8).
CREATE TABLE payments (
  id                 uuid        PRIMARY KEY DEFAULT uuidv7(),
  order_id           uuid        NOT NULL REFERENCES orders (id) ON DELETE RESTRICT,
  provider           text        NOT NULL CHECK (provider IN ('PAYSTACK', 'HUBTEL', 'MANUAL', 'FAKE')),
  network            text        NOT NULL CHECK (network IN ('MTN', 'TELECEL', 'AT', 'CARD', 'CASH')),
  amount_pesewas     bigint      NOT NULL CHECK (amount_pesewas > 0),
  provider_reference text        NOT NULL,
  status             text        NOT NULL CHECK (status IN ('PENDING', 'SUCCESS', 'FAILED', 'REFUNDED')),
  raw_payload        jsonb,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  paid_at            timestamptz,
  -- Webhook idempotency. Scoped by provider so a manual MoMo reference can't clash with a provider's.
  CONSTRAINT payments_provider_reference_key UNIQUE (provider, provider_reference)
);
CREATE INDEX payments_order_id_idx ON payments (order_id);
CREATE TRIGGER payments_set_updated_at BEFORE UPDATE ON payments
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
--rollback DROP TABLE payments;
