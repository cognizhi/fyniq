-- Validates the key and source checks 000300 widened for the privacy notices (PRD-001 CMP-PD-03).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.policy_draft VALIDATE CONSTRAINT policy_draft_key_known;
ALTER TABLE fyniq.policy_version VALIDATE CONSTRAINT policy_version_key_known;
ALTER TABLE fyniq.policy_version VALIDATE CONSTRAINT policy_version_from_format;
