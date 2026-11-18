-- Validates the check 000388 widened for the company secretary assistant's reminders (PRD-001 Appendix E).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.email_message VALIDATE CONSTRAINT email_message_purpose_known;
