-- Validates the check 000354 added for the prices a provider's list gives (PRD-001 AI-82).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_model VALIDATE CONSTRAINT ai_model_listed_price_object;
