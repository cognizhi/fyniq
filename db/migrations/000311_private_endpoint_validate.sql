-- Validates the installation change's steps as 000310 widened them for registered private endpoints (PRD-001 SEC-37).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.installation_change VALIDATE CONSTRAINT installation_change_step_known;
