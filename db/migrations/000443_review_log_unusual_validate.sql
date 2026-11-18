-- Validates the review log's kinds as 000442 widened them for unusual items (PRD-001 FM-6, rev 0.287).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.review_log_entry VALIDATE CONSTRAINT review_log_entry_kind_known;
