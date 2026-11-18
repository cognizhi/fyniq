-- Validate 000377's checks on their own, so changing them never scanned the tables under their locks.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_policy VALIDATE CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_mfa_known;
