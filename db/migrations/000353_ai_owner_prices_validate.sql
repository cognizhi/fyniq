-- Validates the checks 000352 added for the Owners' prices (PRD-001 AI-82).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_price VALIDATE CONSTRAINT ai_price_status_known;
ALTER TABLE fyniq.ai_price VALIDATE CONSTRAINT ai_price_withdrawn_entered;
ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_object_known;
