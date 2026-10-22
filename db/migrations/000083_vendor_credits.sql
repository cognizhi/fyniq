-- Vendor credits (PRD-001 FR-AP-03; p1-purchases task 4.1).
--
-- A supplier's credit note is a document of its own, vendor_credit: versioned as a bill is, posted by the rule
-- vendor_credit@1 (the reverse of a bill's legs) through the posting service, and numbered from its own sequence
-- (FR-ORG-04, VC- by default, gap-free: numbered when it posts). Its entry holds no new kind of line. The widened checks
-- are added NOT VALID and validated by 000084 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation', 'bill', 'bill_payment', 'vendor_credit')) NOT VALID;
ALTER TABLE fyniq.document_sequence DROP CONSTRAINT document_sequence_type_known;
ALTER TABLE fyniq.document_sequence ADD CONSTRAINT document_sequence_type_known
  CHECK (document_type IN ('journal', 'bill', 'bill_payment', 'vendor_credit')) NOT VALID;
