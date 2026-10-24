-- Validates the check 000114 added for allocations' tolerances (PRD-001 FR-BNK-08).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.allocation VALIDATE CONSTRAINT allocation_tolerance;
