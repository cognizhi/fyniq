-- Validates the checks 000348 added for the AI connections' allow-lists (PRD-001 AI-72).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_model VALIDATE CONSTRAINT ai_model_allowed_probed;
ALTER TABLE fyniq.ai_model VALIDATE CONSTRAINT ai_model_probe_together;
ALTER TABLE fyniq.ai_model VALIDATE CONSTRAINT ai_model_probe_detail_length;
ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_object_known;
