-- Validates the check 000105 widened for the emails of notifications and digests (PRD-001 FR-NTF-02).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.email_message VALIDATE CONSTRAINT email_message_purpose_known;
