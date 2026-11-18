-- Validates the long-operation kinds 000325 widened for the workbook import's validation (PRD-001 FR-MIG-01, FR-INT-11).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.operation VALIDATE CONSTRAINT operation_kind_known;
