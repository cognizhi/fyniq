-- The particulars the IR8A asks of a person and Nemura did not hold: sex, the address with its type, the post code or country, and the designation of a non-resident director or
-- seaman (IRAS AIS-API 2.0 data items B6 to B11; PRD-002 NM-PAY-14; p2-payroll-outputs). They are personal data kept for a statutory filing only, so they join the NRIC in the person's
-- restricted record: one JSON document sealed by the cipher port under its own context, read only through the audited, level-checked function with the NRIC's capability, never in
-- an audit event, a list or a log, and excluded from the assistant like the other restricted identifiers.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.person_secret ADD COLUMN ir8a_sealed bytea;

GRANT INSERT (ir8a_sealed), UPDATE (ir8a_sealed) ON nemura.person_secret TO nemura_api;
