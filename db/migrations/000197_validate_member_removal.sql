-- Validates the check 000196 widened for removing a member (PRD-001 FR-ADM-01; rev 0.165).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_object_known;
