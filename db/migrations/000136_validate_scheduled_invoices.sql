-- Validates the checks 000135 added and widened for recurring invoices the scheduler issues and sends (PRD-001 FR-AR-07).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.recurring_invoice VALIDATE CONSTRAINT recurring_invoice_mode_known;
ALTER TABLE fyniq.recurring_invoice VALIDATE CONSTRAINT recurring_invoice_approval_whole;
ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_policy VALIDATE CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.review_log_entry VALIDATE CONSTRAINT review_log_entry_kind_known;
