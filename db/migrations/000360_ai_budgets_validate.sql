-- Validates the check 000359 added to the model calls' cost in the base currency (PRD-001 AI-77).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_call VALIDATE CONSTRAINT ai_call_cost_base;
ALTER TABLE fyniq.ai_call VALIDATE CONSTRAINT ai_call_steps;
