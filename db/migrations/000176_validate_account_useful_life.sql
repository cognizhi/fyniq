-- Validates the useful life's range that 000175 added (PRD-001 FR-COA-02).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.account VALIDATE CONSTRAINT account_useful_life_months;
