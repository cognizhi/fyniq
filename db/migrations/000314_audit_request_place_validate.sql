-- Validates the place of 000313's security events (PRD-001 SEC-31).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.audit_request_detail VALIDATE CONSTRAINT audit_request_detail_country_code;
ALTER TABLE fyniq.audit_request_detail VALIDATE CONSTRAINT audit_request_detail_region_length;
