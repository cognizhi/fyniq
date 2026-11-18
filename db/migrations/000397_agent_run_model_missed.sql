-- A scheduled agent's run keeps why its AI step did not run (PRD-001 AI-64, C.2 #36, rev 0.283; p1-company-secretary task
-- 7.1): the model unavailable, the AI switched off, no route or the budget spent. The run still ends done with its
-- deterministic findings and Fyniq's summary, its model unused, and the watchdog shows it ⚠. Written once, as the run
-- ends, by the worker, as the rest of its report is. The check is validated by 000398.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.agent_run ADD COLUMN model_missed text;
ALTER TABLE fyniq.agent_run ADD CONSTRAINT agent_run_model_missed CHECK (model_missed IS NULL OR (status = 'done' AND model_used IS FALSE
  AND length(model_missed) BETWEEN 1 AND 1000)) NOT VALID;
GRANT UPDATE (model_missed) ON fyniq.agent_run TO fyniq_worker;

-- +goose StatementBegin
-- agent_run_guard as in 000390, and: why its AI step did not run is written as it ends, as the rest of its report is.
CREATE OR REPLACE FUNCTION fyniq.agent_run_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  step text;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'An agent''s run is kept: it is never deleted' USING ERRCODE = 'FY350';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'queued' OR NEW.version <> 1 OR NEW.started_at IS NOT NULL OR NEW.finished_at IS NOT NULL OR NEW.findings IS NOT NULL
       OR NEW.summary IS NOT NULL OR NEW.model_used IS NOT NULL OR NEW.tools IS NOT NULL
       OR NEW.model_missed IS NOT NULL THEN
      RAISE EXCEPTION 'An agent''s run starts queued, at version 1, with no report' USING ERRCODE = 'FY350';
    END IF;
    RETURN NEW;
  END IF;
  IF (NEW.organisation_id, NEW.id, NEW.agent, NEW.skill_version, NEW.skill_sha256, NEW.trigger, NEW.slot_at, NEW.acts_for,
      NEW.requested_by, NEW.queued_at)
     IS DISTINCT FROM
     (OLD.organisation_id, OLD.id, OLD.agent, OLD.skill_version, OLD.skill_sha256, OLD.trigger, OLD.slot_at, OLD.acts_for,
      OLD.requested_by, OLD.queued_at) THEN
    RAISE EXCEPTION 'What an agent''s run is never changes' USING ERRCODE = 'FY350';
  END IF;
  IF NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'Each step of an agent''s run is its next version' USING ERRCODE = 'FY350';
  END IF;
  step := OLD.status || '>' || NEW.status;
  IF step NOT IN ('queued>running', 'queued>skipped', 'running>done', 'running>failed', 'running>skipped') THEN
    RAISE EXCEPTION 'An agent''s run cannot go from % to %', OLD.status, NEW.status USING ERRCODE = 'FY350';
  END IF;
  IF NOT pg_has_role(current_user, 'fyniq_worker', 'MEMBER') THEN
    RAISE EXCEPTION 'Only the worker runs an agent''s run' USING ERRCODE = 'FY350';
  END IF;
  -- Its report is written once, as it ends.
  IF step = 'queued>running' AND (NEW.findings, NEW.summary, NEW.fingerprint, NEW.model_used, NEW.tools, NEW.model_missed) IS DISTINCT FROM
     (OLD.findings, OLD.summary, OLD.fingerprint, OLD.model_used, OLD.tools, OLD.model_missed) THEN
    RAISE EXCEPTION 'An agent''s run reports as it ends' USING ERRCODE = 'FY350';
  END IF;
  IF NEW.started_at IS DISTINCT FROM OLD.started_at AND step <> 'queued>running' THEN
    RAISE EXCEPTION 'An agent''s run starts once' USING ERRCODE = 'FY350';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
