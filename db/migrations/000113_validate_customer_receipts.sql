-- Validates the checks 000112 widened for customer receipts and their approvals (PRD-001 FR-AR-18, FM-1).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document VALIDATE CONSTRAINT document_type_known;
ALTER TABLE fyniq.document_sequence VALIDATE CONSTRAINT document_sequence_type_known;
ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_policy VALIDATE CONSTRAINT approval_policy_object_known;
