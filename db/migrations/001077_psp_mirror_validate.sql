-- Validates the foreign key 001076 added for a provider account's revenue account (PRD-001 FR-PSP-03).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.psp_account VALIDATE CONSTRAINT psp_account_revenue_fkey;
