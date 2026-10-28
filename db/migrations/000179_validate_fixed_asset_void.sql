-- Drops the one-asset-per-line constraint that fixed_asset_source_line_live (000178) replaces, and validates the approval
-- object types 000177 widened (PRD-001 FR-FA-01, FM-1; rev 0.151).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.fixed_asset_source DROP CONSTRAINT fixed_asset_source_line_once;
ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_policy VALIDATE CONSTRAINT approval_policy_object_known;
