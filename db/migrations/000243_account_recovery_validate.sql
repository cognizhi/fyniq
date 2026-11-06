-- Validates the session's step-up methods and the review log's kinds as 000242 widened them for account recovery
-- (PRD-001 SEC-35).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.user_session VALIDATE CONSTRAINT user_session_step_up_method;
ALTER TABLE fyniq.review_log_entry VALIDATE CONSTRAINT review_log_entry_kind_known;
