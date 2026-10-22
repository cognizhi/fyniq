-- Validate the checks 000071 added NOT VALID (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.account VALIDATE CONSTRAINT account_revalue_monetary;
ALTER TABLE fyniq.journal_line VALIDATE CONSTRAINT journal_line_base_amount_rnd_fx;
ALTER TABLE fyniq.document VALIDATE CONSTRAINT document_type_known;
