-- A setting that is a list may be empty and longer (PRD-001 AI-02, AI-32; p1-ai-proposals-core task 1.2): the fields the
-- AI never reads (ai.exclusions) and the task types it may propose (ai.task_types) are empty unless changed, so taking
-- the last one off, or resetting the list, stores an empty value, which 000028's check refused; and fifty of them pass
-- 200 characters. Every other setting keeps a value of 1 to 200 characters.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.organisation_setting DROP CONSTRAINT organisation_setting_value_length;
ALTER TABLE fyniq.organisation_setting ADD CONSTRAINT organisation_setting_value_length CHECK (
  CASE WHEN key IN ('ai.exclusions', 'ai.task_types') THEN length(value) <= 4000 ELSE length(value) BETWEEN 1 AND 200 END
) NOT VALID;
