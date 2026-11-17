-- Validates 000390's check on the tools a run's model step called (PRD-001 CTL-AI-09).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.agent_run VALIDATE CONSTRAINT agent_run_tools;
