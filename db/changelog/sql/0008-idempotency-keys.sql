--liquibase formatted sql

--changeset blinks:0008-idempotency-keys
--comment: Idempotency-Key on order creation: repeating a request returns the first order instead of creating a second.
-- The key is claimed in the same transaction that creates the order, so two identical requests
-- arriving together still make one order. request_hash catches a key reused for a different order.
CREATE TABLE idempotency_keys (
  scope        text        NOT NULL CHECK (scope IN ('ADMIN_CREATE_ORDER', 'CREATE_ORDER')),
  key          text        NOT NULL CHECK (length(key) BETWEEN 1 AND 64),
  request_hash bytea       NOT NULL,
  order_id     uuid        REFERENCES orders (id) ON DELETE CASCADE,
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (scope, key)
);
--rollback DROP TABLE idempotency_keys;
