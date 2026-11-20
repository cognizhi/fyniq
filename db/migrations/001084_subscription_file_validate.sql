-- Validates the document-type check 001083 widened for a subscription contract's files (PRD-001 FR-AR-15).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document VALIDATE CONSTRAINT document_type_known;
