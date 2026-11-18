-- Validates the account roles 000328 widened for opening open items (PRD-001 FR-GL-06).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.account VALIDATE CONSTRAINT account_role_known;
