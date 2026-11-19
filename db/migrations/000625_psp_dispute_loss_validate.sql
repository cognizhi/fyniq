-- Validates the provider account's loss-account foreign key of 000624 (squawk: constraint-missing-not-valid).

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
ALTER TABLE fyniq.psp_account VALIDATE CONSTRAINT psp_account_loss_fkey;
