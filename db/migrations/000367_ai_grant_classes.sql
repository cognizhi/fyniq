-- What a turn has read (PRD-001 AI-83, AI-75; p1-ai-assistant task 1.2, design D4).
--
-- A turn's grant keeps the data classes of everything the turn read through it. The model call the AI service asks the
-- API for is routed for those classes as well as the ones the AI service declares, so the AI service can never send a
-- class it read to a connection the data-class policy keeps it from. The classes only widen, and only while the grant is
-- live.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_grant ADD COLUMN classes text[] NOT NULL DEFAULT '{}';
ALTER TABLE fyniq.ai_grant ADD CONSTRAINT ai_grant_classes
  CHECK (classes <@ ARRAY['know-how', 'business-figures', 'personal-data', 'payroll']::text[]) NOT VALID;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.ai_grant_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (NEW.organisation_id, NEW.id, NEW.token_hash, NEW.user_id, NEW.turn_id, NEW.issued_at, NEW.expires_at)
     IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.token_hash, OLD.user_id, OLD.turn_id, OLD.issued_at, OLD.expires_at)
     OR OLD.ended_at IS NOT NULL OR NOT NEW.classes @> OLD.classes THEN
    RAISE EXCEPTION 'A delegated read grant only ends, once, and what it read only widens before then' USING ERRCODE = 'FY340';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT UPDATE (classes) ON fyniq.ai_grant TO fyniq_api;
