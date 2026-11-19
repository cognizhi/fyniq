-- The allowlist of AI apps may be empty (PRD-001 AI-35; p1-mcp-first-party task 2.1): mcp.clients is empty unless an Owner
-- allows an app, and taking the last app off, which is the per-app kill switch and takes effect at once, stores an empty value,
-- which 000028's check refused. Like the two lists 000373 let be empty, it is kept to 4,000 characters; every other setting keeps
-- a value of 1 to 200 characters.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.organisation_setting DROP CONSTRAINT organisation_setting_value_length;
ALTER TABLE fyniq.organisation_setting ADD CONSTRAINT organisation_setting_value_length CHECK (
  CASE WHEN key IN ('ai.exclusions', 'ai.task_types', 'mcp.clients') THEN length(value) <= 4000 ELSE length(value) BETWEEN 1 AND 200 END
) NOT VALID;
