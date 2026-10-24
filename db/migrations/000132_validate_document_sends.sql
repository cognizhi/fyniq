-- Validates the checks 000131 widened for generated evidence and the emails of invoices and quotes (PRD-001 FR-AR-06).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.evidence VALIDATE CONSTRAINT evidence_source_known;
ALTER TABLE fyniq.email_message VALIDATE CONSTRAINT email_message_purpose_known;
