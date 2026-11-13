-- Validates the checks 000341 widened for the full tenant export (PRD-001 FR-MIG-04).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.report_export VALIDATE CONSTRAINT report_export_report_known;
ALTER TABLE fyniq.report_export VALIDATE CONSTRAINT report_export_format_known;
ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_policy VALIDATE CONSTRAINT approval_policy_object_known;
