-- Validates the constraint 000334 widened NOT VALID: the difference register's line form, with the open items and the
-- payroll totals (p1-workbook-migration task 5.2).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.migration_difference VALIDATE CONSTRAINT migration_difference_line_format;
