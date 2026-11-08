-- Validates the check 000281 added for the kinds of explanation (PRD-001 FR-GL-14).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.period_variance_explanation VALIDATE CONSTRAINT period_variance_explanation_kind_known;
