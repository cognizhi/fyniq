-- Validates the money account's payment file check that 000430 added (PRD-001 FR-BNK-12, rev 0.157).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.money_account VALIDATE CONSTRAINT money_account_payment_file_known;
