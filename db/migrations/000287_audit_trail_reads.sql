-- The API reads its organisation's audit trail (PRD-001 AUD-03, AUD-07, FR-CTL-03; p1-audit-explorer slice 5).
--
-- Verify integrity and the audit trail explorer are API operations, for those who hold V on Appendix D's "Audit trail"
-- row. Until now only the worker and the read-only role read the trail (000006). The API reads it now, and only under
-- row-level security, so one organisation's trail at a time; who may call is the operations' policy. It still writes
-- only through the unit of work, and never updates or deletes.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

GRANT SELECT ON fyniq.audit_event, fyniq.audit_chain, fyniq.audit_request_detail TO fyniq_api;
