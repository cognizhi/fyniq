-- Validates the checks and foreign keys 000234 added for Owner succession (PRD-001 FR-ADM-09).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document VALIDATE CONSTRAINT document_type_known;
ALTER TABLE fyniq.owner_succession VALIDATE CONSTRAINT owner_succession_document_fkey;
ALTER TABLE fyniq.owner_succession VALIDATE CONSTRAINT owner_succession_outgoing_fkey;
ALTER TABLE fyniq.owner_succession VALIDATE CONSTRAINT owner_succession_successor_fkey;
ALTER TABLE fyniq.owner_succession VALIDATE CONSTRAINT owner_succession_outgoing_user_fkey;
ALTER TABLE fyniq.owner_succession VALIDATE CONSTRAINT owner_succession_started_by_fkey;
ALTER TABLE fyniq.owner_succession VALIDATE CONSTRAINT owner_succession_ended_by_fkey;
