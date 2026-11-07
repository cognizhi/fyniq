-- Validates the role check and foreign key 000257 added for 2700's lines keyed by instrument (PRD-001 FR-EQ-18).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.account VALIDATE CONSTRAINT account_role_known;
ALTER TABLE fyniq.journal_line VALIDATE CONSTRAINT journal_line_instrument_fkey;
