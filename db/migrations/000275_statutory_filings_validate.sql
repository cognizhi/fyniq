-- Validates the check 000274 widened for filing records (PRD-001 FR-TAX-07).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document VALIDATE CONSTRAINT document_type_known;
