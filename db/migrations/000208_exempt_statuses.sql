-- The exempt-private-company status (PRD-001 FR-EQ-11; p1-founders-money task 3.3, design D9; rev 0.167).
--
-- Until the corporate profile exists (FR-CS-01), the organisation's exempt-private-company status from a day on is an
-- exempt_status document: a draft takes its evidence (the ACRA business profile), and recording it posts it with no
-- entry. Posted, it is immutable (append-only); a later one supersedes it from its day. The widened check is added NOT
-- VALID and validated by 000209 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation', 'bill', 'bill_payment', 'vendor_credit', 'credit_application',
                  'supplier_refund', 'purchase_order', 'invoice', 'customer_receipt', 'write_off', 'credit_note',
                  'customer_credit_application', 'customer_refund', 'customer_deposit', 'quote', 'sales_order', 'statement',
                  'bank_transaction', 'bank_agreement', 'expense_claim', 'claim_reimbursement', 'cash_advance', 'fixed_asset',
                  'asset_disposal', 'payee_bank_change', 'claimant_bank_change', 'payment_batch',
                  'pre_incorporation_cost', 'director_pay_choice', 'director_fee', 'exempt_status')) NOT VALID;
