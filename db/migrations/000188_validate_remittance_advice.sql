-- Validates the check 000187 widened for remittance advice (PRD-001 FR-BNK-12; rev 0.154).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.email_message VALIDATE CONSTRAINT email_message_purpose_known;
