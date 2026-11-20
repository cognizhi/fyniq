-- Validate the delegation constraints added NOT VALID in 001200 (PRD-001 FR-APR-06): no signature had a delegator before it.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.approval_signature VALIDATE CONSTRAINT approval_signature_on_behalf_fkey;
ALTER TABLE fyniq.approval_signature VALIDATE CONSTRAINT approval_signature_on_behalf_other;
