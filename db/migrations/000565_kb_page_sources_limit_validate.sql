-- Validates 000564's check.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.kb_page_version VALIDATE CONSTRAINT kb_page_version_sources;
