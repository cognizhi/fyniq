-- Validates the request checks that 000039 widened NOT VALID, in a transaction of its own (squawk:
-- constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_policy_steps_valid;
ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_object_known;
