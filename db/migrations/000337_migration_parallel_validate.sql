-- Validates the constraint 000336 widened NOT VALID: the migration sign-off's subjects, now mapping, reconciliation and
-- parallel (p1-workbook-migration task 6.1).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.migration_signoff VALIDATE CONSTRAINT migration_signoff_subject_known;
