-- Validates the check 000367 added to the delegated read grants' classes (PRD-001 AI-83).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_grant VALIDATE CONSTRAINT ai_grant_classes;
