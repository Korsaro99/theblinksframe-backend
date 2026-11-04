--liquibase formatted sql

--changeset blinks:0009-payments-attempt-details
--comment: What the "approve on your phone" screen shows for an online payment attempt (GET /orders/{id}/payment).
ALTER TABLE payments
  ADD COLUMN failure_reason text,
  ADD COLUMN next_action    text CHECK (next_action IN ('APPROVE_ON_PHONE', 'REDIRECT')),
  ADD COLUMN redirect_url   text;
--rollback ALTER TABLE payments DROP COLUMN failure_reason, DROP COLUMN next_action, DROP COLUMN redirect_url;

--changeset blinks:0009-payments-one-pending-per-order
--comment: At most one payment attempt in flight per order, so two quick taps on Pay can't send two prompts or charge twice.
CREATE UNIQUE INDEX payments_one_pending_per_order ON payments (order_id) WHERE status = 'PENDING';
--rollback DROP INDEX payments_one_pending_per_order;

--changeset blinks:0009-payments-pending-idx
--comment: The housekeeping job checks pending attempts with the provider, oldest first.
CREATE INDEX payments_pending_created_at_idx ON payments (created_at) WHERE status = 'PENDING';
--rollback DROP INDEX payments_pending_created_at_idx;
