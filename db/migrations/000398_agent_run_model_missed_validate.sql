-- Validates the check 000397 added for why a run's AI step did not run (PRD-001 AI-64, C.2 #36, rev 0.283).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.agent_run VALIDATE CONSTRAINT agent_run_model_missed;
