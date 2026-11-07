-- Validates the checks 000250 widened and added for holders and share classes (PRD-001 FR-EQ-15, FR-EQ-16).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.contact VALIDATE CONSTRAINT contact_role_known;
ALTER TABLE fyniq.contact VALIDATE CONSTRAINT contact_member_person;
ALTER TABLE fyniq.document VALIDATE CONSTRAINT document_type_known;
ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_policy VALIDATE CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.share_issue VALIDATE CONSTRAINT share_issue_class_document_fkey;
