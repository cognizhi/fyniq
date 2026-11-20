-- Validates the source check 001124 widened for the platform's own compliance tasks (PRD-001 ENG-33).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.compliance_task VALIDATE CONSTRAINT compliance_task_source;
