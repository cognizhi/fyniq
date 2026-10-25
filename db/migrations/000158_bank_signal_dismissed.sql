-- A pending signal a person dismisses (PRD-001 FR-BNK-16, UC-BNK-02; p1-bank-import-reconcile task 7.3, design D10): an
-- incoming-funds alert that points to the wrong invoice, or to none that will come, is dismissed with a reason, kept in
-- the audit trail, and stops showing "likely paid". Like a closed or expired signal it never changes again (FY225).
-- The widened check is added NOT VALID and validated by 000159 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.bank_signal DROP CONSTRAINT bank_signal_status_known;
ALTER TABLE fyniq.bank_signal ADD CONSTRAINT bank_signal_status_known
  CHECK (status IN ('pending', 'closed', 'expired', 'dismissed')) NOT VALID;
