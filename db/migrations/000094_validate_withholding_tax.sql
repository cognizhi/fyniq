-- Validates the checks 000093 added or widened for withholding tax (PRD-001 FR-AP-19).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.contact VALIDATE CONSTRAINT contact_tax_residence_format;
ALTER TABLE fyniq.account VALIDATE CONSTRAINT account_role_known;
