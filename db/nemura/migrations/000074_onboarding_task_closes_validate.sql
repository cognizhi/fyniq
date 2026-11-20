-- Validates what 000073 added NOT VALID: the flows an onboarding task may close by.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.onboarding_template_task VALIDATE CONSTRAINT onboarding_template_task_closes_known;
ALTER TABLE nemura.onboarding_task VALIDATE CONSTRAINT onboarding_task_closes_known;
