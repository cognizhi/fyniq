-- The privacy notices in the policy library (PRD-001 CMP-PD-03, rev 0.244; p1-pdpa-retention slice 3, design D2): the
-- members' and the contacts' notices are two more keys of the library, drafted, published and versioned as policies are,
-- under the same guards (FY310). The key checks widen; 000301 validates them.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.policy_draft DROP CONSTRAINT policy_draft_key_known;
ALTER TABLE fyniq.policy_draft ADD CONSTRAINT policy_draft_key_known CHECK (key IN ('information-security', 'acceptable-use', 'access-control', 'incident-response',
    'backup', 'change-management', 'ai-use', 'data-protection', 'notice-members', 'notice-contacts')) NOT VALID;
ALTER TABLE fyniq.policy_version DROP CONSTRAINT policy_version_key_known;
ALTER TABLE fyniq.policy_version ADD CONSTRAINT policy_version_key_known CHECK (key IN ('information-security', 'acceptable-use', 'access-control', 'incident-response',
    'backup', 'change-management', 'ai-use', 'data-protection', 'notice-members', 'notice-contacts')) NOT VALID;
ALTER TABLE fyniq.policy_version DROP CONSTRAINT policy_version_from_format;
ALTER TABLE fyniq.policy_version ADD CONSTRAINT policy_version_from_format
  CHECK (from_draft ~ '^(fyniq-(policy|notice)-draft@[0-9]+|revision [0-9]+)$') NOT VALID;
