-- The NRIC pattern scan (PRD-001 FR-DOC-09; rev 0.183; p1-documents-evidence task 3.2): a file whose text shows a
-- Singapore NRIC or FIN in full is flagged when it is attached, and the flag stays with it. Files attached before the
-- scan read false; they were not scanned.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.evidence ADD COLUMN nric_flagged boolean NOT NULL DEFAULT false;
GRANT INSERT (nric_flagged) ON fyniq.evidence TO fyniq_api, fyniq_worker;
