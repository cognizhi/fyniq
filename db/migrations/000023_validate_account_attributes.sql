-- Validates the account-attribute checks that 000022 added NOT VALID, in a transaction of their own so the
-- scan holds only a SHARE UPDATE EXCLUSIVE lock (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.account VALIDATE CONSTRAINT account_attribute_lengths;
ALTER TABLE fyniq.account VALIDATE CONSTRAINT account_function_known;
ALTER TABLE fyniq.account VALIDATE CONSTRAINT account_gst_code_format;
ALTER TABLE fyniq.account VALIDATE CONSTRAINT account_currency_format;
