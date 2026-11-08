-- Validates the check 000229 added for the rules of approval policies (PRD-001 FR-APR-01).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.approval_policy VALIDATE CONSTRAINT approval_policy_rules_valid;
