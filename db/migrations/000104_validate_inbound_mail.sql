-- Validates the check 000103 added to the people's verified sign-in emails (PRD-001 FR-INT-22).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.user_identity VALIDATE CONSTRAINT user_identity_email_form;
