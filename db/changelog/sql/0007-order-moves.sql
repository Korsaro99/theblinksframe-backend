--liquibase formatted sql

--changeset blinks:0007-order-items-stock-deducted
--comment: Which order lines took stock, so a cancel gives back exactly what was taken (BRIEF §7.2).
-- A paid order whose stock had already run out keeps stock_deducted = false for the short line
-- and is flagged with needs_attention; cancelling it must not add back pairs that never left.
ALTER TABLE order_items ADD COLUMN stock_deducted boolean NOT NULL DEFAULT false;
-- Orders already past payment took their stock when they were paid.
UPDATE order_items i SET stock_deducted = true
  FROM orders o
  WHERE o.id = i.order_id
    AND o.status IN ('PAID', 'PACKED', 'OUT_FOR_DELIVERY', 'READY_FOR_PICKUP', 'DELIVERED');
--rollback ALTER TABLE order_items DROP COLUMN stock_deducted;

--changeset blinks:0007-order-events-undo
--comment: An undo is recorded as a new event that points at the event it reverses (POST /admin/orders/{id}/undo).
-- UNIQUE: an event can be undone at most once, even if two undo requests race.
ALTER TABLE order_events
  ADD COLUMN undoes_event_id uuid UNIQUE REFERENCES order_events (id) ON DELETE CASCADE;
--rollback ALTER TABLE order_events DROP COLUMN undoes_event_id;
