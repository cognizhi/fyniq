-- Validates the cardholder's foreign key and check that 000170 added (PRD-001 FR-AP-18).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.money_account VALIDATE CONSTRAINT money_account_cardholder_fkey;
ALTER TABLE fyniq.money_account VALIDATE CONSTRAINT money_account_cardholder_card;
