-- Validates the check 000298 added for the DPO's contact (PRD-001 CMP-PD-01).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.organisation VALIDATE CONSTRAINT organisation_dpo_contact;
