-- Validates the checks 000182 widened or added for claimants' bank details (PRD-001 FR-AP-22; rev 0.154).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document VALIDATE CONSTRAINT document_type_known;
ALTER TABLE fyniq.payee_bank_account VALIDATE CONSTRAINT payee_bank_account_one_payee;
ALTER TABLE fyniq.payee_bank_account VALIDATE CONSTRAINT payee_bank_account_member_fkey;
ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_policy VALIDATE CONSTRAINT approval_policy_object_known;
