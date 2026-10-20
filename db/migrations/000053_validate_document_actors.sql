-- Validates the constraints that 000052 added NOT VALID, in a transaction of its own (squawk:
-- constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document_version VALIDATE CONSTRAINT document_version_created_by_format;
ALTER TABLE fyniq.document VALIDATE CONSTRAINT document_posted_by_format;
ALTER TABLE fyniq.document VALIDATE CONSTRAINT document_posted_together;
