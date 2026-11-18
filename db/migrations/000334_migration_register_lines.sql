-- The rest of M4's parity in the difference register (PRD-001 §17.1 M4, FR-MIG-02 rev 0.267; p1-workbook-migration task
-- 5.2): besides an account's figure and a workbook check, a line is what a customer owes or a supplier is owed at the
-- as-of, by the contact's ID (or others, the workbook's contacts not on Contacts), and a month's payroll total: net pay,
-- what is due to the CPF Board, or the employer's cost. The line's form is widened NOT VALID and validated by 000335.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.migration_difference DROP CONSTRAINT migration_difference_line_format;
ALTER TABLE fyniq.migration_difference ADD CONSTRAINT migration_difference_line_format CHECK (line_key ~ (
  '^([0-9A-Za-z_-]{1,20}\.(opening|[0-9]{4}-(0[1-9]|1[0-2])\.(movement|closing))|check\.[0-9]{1,3}'
  || '|a[rp]\.([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|others)'
  || '|payroll\.[0-9]{4}-(0[1-9]|1[0-2])\.(net|statutory|cost))$')) NOT VALID;
