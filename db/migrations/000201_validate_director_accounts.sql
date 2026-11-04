-- Validates the checks and keys 000199 widened or added for director accounts (PRD-001 FR-EQ-01, B.7; rev 0.167).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.contact VALIDATE CONSTRAINT contact_role_known;
ALTER TABLE fyniq.contact VALIDATE CONSTRAINT contact_member_person;
ALTER TABLE fyniq.contact VALIDATE CONSTRAINT contact_member_fkey;
ALTER TABLE fyniq.journal_line VALIDATE CONSTRAINT journal_line_party_fkey;
ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_policy VALIDATE CONSTRAINT approval_policy_object_known;
