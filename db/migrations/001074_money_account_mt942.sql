-- MT942, the bank's intraday report, as a format a money account's reports come in (PRD-001 FR-BNK-15, FR-BNK-17, rev 0.522; p2-bank-feeds-payments
-- task 2.2). Its lines are pending signals, never statement lines (FR-BNK-16): the daily MT940 closes them. The check is widened NOT VALID here
-- and validated by the next migration.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.money_account_source DROP CONSTRAINT money_account_source_format_known;
ALTER TABLE fyniq.money_account_source ADD CONSTRAINT money_account_source_format_known
  CHECK (report_format IN ('csv', 'xlsx', 'ofx', 'camt053', 'mt940', 'mt940_sap', 'mt942')) NOT VALID;
