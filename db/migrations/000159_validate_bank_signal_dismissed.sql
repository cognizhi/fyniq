-- Validates the signal statuses 000158 widened for a person's dismissal (PRD-001 FR-BNK-16).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.bank_signal VALIDATE CONSTRAINT bank_signal_status_known;
