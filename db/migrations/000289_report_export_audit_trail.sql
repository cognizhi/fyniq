-- The audit trail's export (PRD-001 FR-CTL-03, AUD-07, AUD-05; p1-audit-explorer slice 6, design D6).
--
-- An export of the audit trail is a report export of its own kind, audit_trail, asked for by requestAuditTrailExport
-- (V on Appendix D's "Audit trail") and prepared by the worker like the accountant exports. The check of the known
-- reports is widened NOT VALID, and 000290 validates it (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.report_export DROP CONSTRAINT report_export_report_known;
ALTER TABLE fyniq.report_export ADD CONSTRAINT report_export_report_known
  CHECK (report IN ('general_ledger', 'trial_balance', 'journals', 'fixed_assets', 'receivables', 'payables', 'audit_trail')) NOT VALID;
