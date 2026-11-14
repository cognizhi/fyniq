-- Validates the checks 000364 added to the AI budgets and reservations (PRD-001 AI-13, AI-86).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_budget VALIDATE CONSTRAINT ai_budget_scope_known;
ALTER TABLE fyniq.ai_budget VALIDATE CONSTRAINT ai_budget_subject;
ALTER TABLE fyniq.ai_budget VALIDATE CONSTRAINT ai_budget_tokens_whole;
ALTER TABLE fyniq.ai_reservation VALIDATE CONSTRAINT ai_reservation_tokens;
