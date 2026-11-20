-- Validates the check 001081 added for the costs left out of a project's progress (PRD-001 FR-PRJ-11).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.project_revenue_election VALIDATE CONSTRAINT project_revenue_election_excluded_form;
