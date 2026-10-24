-- Validates the checks 000128 widened for quotes and sales orders (PRD-001 FR-AR-01).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document VALIDATE CONSTRAINT document_type_known;
ALTER TABLE fyniq.document_sequence VALIDATE CONSTRAINT document_sequence_type_known;
