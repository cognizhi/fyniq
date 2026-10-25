-- Validates the checks 000146 widened for categorised bank lines (PRD-001 FR-BNK-06).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document VALIDATE CONSTRAINT document_type_known;
ALTER TABLE fyniq.bank_match VALIDATE CONSTRAINT bank_match_kind_refs;
