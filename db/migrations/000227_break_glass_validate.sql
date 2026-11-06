-- Validates the checks and foreign keys 000226 widened and added for break-glass (PRD-001 FM-8).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.payment_batch_release VALIDATE CONSTRAINT payment_batch_release_break_glass_fkey;
ALTER TABLE fyniq.payment_batch_release VALIDATE CONSTRAINT payment_batch_release_approved_once;
ALTER TABLE fyniq.review_log_entry VALIDATE CONSTRAINT review_log_entry_kind_known;
