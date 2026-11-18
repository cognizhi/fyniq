-- Validates the checks 000350 added for the models' prices (PRD-001 AI-82).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_model VALIDATE CONSTRAINT ai_model_unpriced_checked;
ALTER TABLE fyniq.ai_model VALIDATE CONSTRAINT ai_model_unpriced_length;
