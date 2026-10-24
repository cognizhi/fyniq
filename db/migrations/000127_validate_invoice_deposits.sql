-- Validates the check 000126 added on the output tax a use of a customer deposit takes (PRD-001 FR-AR-09).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.allocation VALIDATE CONSTRAINT allocation_tax;
