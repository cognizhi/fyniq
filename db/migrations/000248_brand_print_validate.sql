-- Validates the brand version's logo for documents and emails as 000247 added it (PRD-001 FR-ORG-09).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.brand_version VALIDATE CONSTRAINT brand_version_print;
