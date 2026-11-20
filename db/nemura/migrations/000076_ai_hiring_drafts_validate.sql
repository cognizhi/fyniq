-- Validates what 000075 widened NOT VALID: the kinds of draft the executor attaches and the task types an Owner may turn on.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.ai_draft VALIDATE CONSTRAINT ai_draft_kind;
ALTER TABLE nemura.ai_settings VALIDATE CONSTRAINT ai_settings_task_types_known;
ALTER TABLE nemura.ai_proposal VALIDATE CONSTRAINT ai_proposal_task_known;
