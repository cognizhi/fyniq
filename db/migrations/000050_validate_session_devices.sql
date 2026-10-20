-- Validates the constraints that 000049 added NOT VALID, in a transaction of its own (squawk:
-- constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.user_session VALIDATE CONSTRAINT user_session_user_agent_length;
ALTER TABLE fyniq.email_message VALIDATE CONSTRAINT email_message_purpose_known;
