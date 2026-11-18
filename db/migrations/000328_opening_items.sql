-- Opening open items (PRD-001 FR-GL-06, Appendix B.2 "Opening open items", rev 0.257; p1-workbook-migration task 3.1,
-- design D6) [accountant to confirm]. An invoice or bill marked opening_item, issued or received before the books start
-- and unpaid then, posts at the books start under opening_invoice@1 or opening_bill@1 against 3900 Opening balance
-- clearing, which takes the system role opening-balance-clearing. The widened role check is added NOT VALID and
-- validated by 000329.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.account DROP CONSTRAINT account_role_known;
ALTER TABLE fyniq.account ADD CONSTRAINT account_role_known CHECK (role IN ('accounts-receivable', 'accounts-payable', 'gst-control',
  'fx-rounding', 'unrealised-fx', 'deferred-revenue', 'prepayments', 'salaries-payable', 'cpf-sdl-payable', 'employer-cpf', 'sdl',
  'depreciation', 'accumulated-depreciation', 'suspense', 'retained-earnings', 'expense-claims-payable', 'income-tax-payable',
  'income-tax-expense', 'director-loan', 'deferred-remuneration', 'share-capital', 'capital-contribution',
  'withholding-tax-payable', 'customer-deposits', 'share-application-monies', 'convertible-instruments',
  'opening-balance-clearing')) NOT VALID;
