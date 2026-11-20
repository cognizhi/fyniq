-- Validates 000056's check (squawk: constraint-missing-not-valid).

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.bank_account_change VALIDATE CONSTRAINT bank_account_change_bank_version;
