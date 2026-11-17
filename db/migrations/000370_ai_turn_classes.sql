-- The classes of data a turn read (PRD-001 AI-12, AI-83, FR-PAY-13; p1-ai-assistant task 6.2): kept with its answer, from
-- its delegated grant as the turn ends, so the AI activity log shows them and masks a turn that carried payroll to a
-- reader who may not see payroll. A turn's classes are set as it is answered, with the rest of its end; the guard of
-- 000369 keeps them from changing after.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_turn ADD COLUMN classes text[] NOT NULL DEFAULT '{}';
ALTER TABLE fyniq.ai_turn ADD CONSTRAINT ai_turn_classes
  CHECK (classes <@ ARRAY['know-how', 'business-figures', 'personal-data', 'payroll', 'restricted']::text[]) NOT VALID;
GRANT UPDATE (classes) ON fyniq.ai_turn TO fyniq_api;
