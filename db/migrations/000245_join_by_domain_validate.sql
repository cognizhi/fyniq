-- Validates the checks 000244 added for joining by domain (PRD-001 FR-ADM-08).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.sign_in_method VALIDATE CONSTRAINT sign_in_method_join_needs_domains;
ALTER TABLE fyniq.user_session VALIDATE CONSTRAINT user_session_email_length;
