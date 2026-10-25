-- Validates the checks 000151 added for auto-reconcile's undo and widened for its weekly sample (PRD-001 FR-BNK-14).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.review_log_entry VALIDATE CONSTRAINT review_log_entry_kind_known;
ALTER TABLE fyniq.bank_transfer VALIDATE CONSTRAINT bank_transfer_undone_whole;
ALTER TABLE fyniq.bank_transfer VALIDATE CONSTRAINT bank_transfer_undo_reason_length;
ALTER TABLE fyniq.bank_transfer VALIDATE CONSTRAINT bank_transfer_undone_by_form;
