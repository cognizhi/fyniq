-- Validates the checks 000153 added for the bank-ingest address (PRD-001 FR-INT-22, FR-BNK-17).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.capture_address VALIDATE CONSTRAINT capture_address_kind_known;
ALTER TABLE fyniq.capture_address VALIDATE CONSTRAINT capture_address_bank_organisation;
ALTER TABLE fyniq.capture_sender VALIDATE CONSTRAINT capture_sender_kind_known;
ALTER TABLE fyniq.capture_sender VALIDATE CONSTRAINT capture_sender_bank_organisation;
