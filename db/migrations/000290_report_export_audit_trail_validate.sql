-- Validates the check 000289 widened for the audit trail's export (PRD-001 FR-CTL-03).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.report_export VALIDATE CONSTRAINT report_export_report_known;
