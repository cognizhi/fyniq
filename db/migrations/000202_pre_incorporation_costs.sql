-- Costs before the books start (PRD-001 FR-EQ-03; p1-founders-money task 2.2, design D7).
--
-- A cost a director or shareholder paid before the books start is a pre_incorporation_cost document, numbered PIC-: a
-- draft needs the accountant's decision and posts nothing; the decision is its next version, which posts it under
-- pre_incorporation_cost@1 (Dr the account decided · Cr 2500, naming the person, which journal_line_check already
-- requires: FY250). Its receipt is evidence on the document, as a claim's is. The widened checks are added NOT VALID and
-- validated by 000203 (squawk: constraint-missing-not-valid).

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
                  'pre_incorporation_cost')) NOT VALID;
ALTER TABLE fyniq.document_sequence DROP CONSTRAINT document_sequence_type_known;
ALTER TABLE fyniq.document_sequence ADD CONSTRAINT document_sequence_type_known
  CHECK (document_type IN ('journal', 'bill', 'bill_payment', 'vendor_credit', 'supplier_refund', 'purchase_order', 'invoice',
                           'customer_receipt', 'write_off', 'credit_note', 'customer_refund', 'customer_deposit', 'quote',
                           'sales_order', 'statement', 'expense_claim', 'claim_reimbursement', 'cash_advance', 'fixed_asset',
                           'asset_disposal', 'payment_batch', 'pre_incorporation_cost')) NOT VALID;
