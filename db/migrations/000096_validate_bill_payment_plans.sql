-- Validates the check 000095 added for a supplier's early-payment discount terms (PRD-001 FR-AP-13).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.contact VALIDATE CONSTRAINT contact_discount_terms;
