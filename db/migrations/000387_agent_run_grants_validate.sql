-- Validates 000386's constraints apart, so adding them took no long lock (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_grant VALIDATE CONSTRAINT ai_grant_origin;
ALTER TABLE fyniq.ai_grant VALIDATE CONSTRAINT ai_grant_run_fkey;
ALTER TABLE fyniq.ai_grant VALIDATE CONSTRAINT ai_grant_lifetime;
ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_mfa_known;
