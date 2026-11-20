-- Validates 000912's widened closed lists (squawk: constraint-missing-not-valid).

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_policy VALIDATE CONSTRAINT approval_policy_object_known;
