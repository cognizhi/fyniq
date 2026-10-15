-- Validates account_role_known, which 000014 added NOT VALID, in a transaction of its own so the
-- check of existing rows does not hold the lock of the ALTER that added it (squawk).

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
ALTER TABLE fyniq.account VALIDATE CONSTRAINT account_role_known;
