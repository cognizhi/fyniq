-- Validates the check 001074 widened for MT942 intraday reports (PRD-001 FR-BNK-15).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.money_account_source VALIDATE CONSTRAINT money_account_source_format_known;
