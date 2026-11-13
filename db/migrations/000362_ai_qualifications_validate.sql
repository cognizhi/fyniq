-- Validates the check 000361 changed for the eval runner's calls (PRD-001 AI-78).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_call VALIDATE CONSTRAINT ai_call_steps;
