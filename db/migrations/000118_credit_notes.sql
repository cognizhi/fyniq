-- Credit notes to customers (PRD-001 FR-AR-04; p1-sales task 3.1, part).
--
-- A credit note to a customer is a document of its own, credit_note: versioned as an invoice is, linked to the invoice
-- it credits or standalone, posted (issued) by the rule credit_note@1 (the reverse of an invoice's legs) through the
-- posting service, and numbered from its own sequence (FR-ORG-04, CN- by default, gap-free: numbered when it is
-- issued). Its entry holds no new kind of line, and its approvals are the invoice's (the object type invoice, on
-- "Invoices, credit notes"). The widened checks are added NOT VALID and validated by 000119 (squawk:
-- constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation', 'bill', 'bill_payment', 'vendor_credit', 'credit_application',
                  'supplier_refund', 'purchase_order', 'invoice', 'customer_receipt', 'write_off', 'credit_note')) NOT VALID;
ALTER TABLE fyniq.document_sequence DROP CONSTRAINT document_sequence_type_known;
ALTER TABLE fyniq.document_sequence ADD CONSTRAINT document_sequence_type_known
  CHECK (document_type IN ('journal', 'bill', 'bill_payment', 'vendor_credit', 'supplier_refund', 'purchase_order', 'invoice',
                           'customer_receipt', 'write_off', 'credit_note')) NOT VALID;
