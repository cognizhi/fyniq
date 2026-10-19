-- The account attributes of PRD-001 FR-COA-02 (p1-organisation-chart task 3.1): the report group and
-- cash-flow line the statements use, the function (COGS, R&D, S&M, G&A), the money-account flag, the default
-- GST code, the currency of a bank account, the control-account flag (an account with a sub-ledger), the
-- payroll-cost flag, and the usage guidance shown at entry. The standard chart (internal/domain/chart,
-- rev 0.31) sets them when an organisation loads it. Changing them, and protecting the control and
-- payroll-cost flags, comes with task 3.2.
--
-- The checks are added NOT VALID and validated by 000023 in a transaction of their own (squawk:
-- constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.account
  ADD COLUMN report_group     text,
  ADD COLUMN cash_flow_line   text,
  ADD COLUMN function         text,
  ADD COLUMN is_money_account boolean NOT NULL DEFAULT false,
  ADD COLUMN default_gst_code text,
  ADD COLUMN currency         text,
  ADD COLUMN is_control       boolean NOT NULL DEFAULT false,
  ADD COLUMN is_payroll_cost  boolean NOT NULL DEFAULT false,
  ADD COLUMN guidance         text;

ALTER TABLE fyniq.account
  ADD CONSTRAINT account_attribute_lengths CHECK (length(report_group) BETWEEN 1 AND 60
    AND length(cash_flow_line) BETWEEN 1 AND 60 AND length(guidance) BETWEEN 1 AND 500) NOT VALID,
  ADD CONSTRAINT account_function_known CHECK (function IN ('COGS', 'R&D', 'S&M', 'G&A', 'Other')) NOT VALID,
  -- The default GST code is one of the sg-gst pack's codes; the pack arrives with p1-statutory-packs, so
  -- only the form is checked here.
  ADD CONSTRAINT account_gst_code_format CHECK (default_gst_code ~ '^[A-Z]{2,4}$') NOT VALID,
  ADD CONSTRAINT account_currency_format CHECK (currency ~ '^[A-Z]{3}$') NOT VALID;

GRANT INSERT (report_group, cash_flow_line, function, is_money_account, default_gst_code, currency, is_control, is_payroll_cost, guidance)
  ON fyniq.account TO fyniq_api;
