-- Validates the check 000079 widened (PRD-001 FR-AUT-01).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.job_slot VALIDATE CONSTRAINT job_slot_job_format;
