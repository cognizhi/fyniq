-- Validates 000587's constraints apart, so adding them took no long lock (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_proposal VALIDATE CONSTRAINT ai_proposal_mcp_app;
ALTER TABLE fyniq.ai_proposal VALIDATE CONSTRAINT ai_proposal_originator;
ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_mfa_known;
