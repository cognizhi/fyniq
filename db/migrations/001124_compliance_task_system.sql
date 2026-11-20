-- A compliance task the platform raises itself (PRD-001 ENG-33, rev 0.611; p2-tax-filings task 4.1): a newer version of an activated statutory pack is waiting (pack.update_available), or a
-- published correction touches a past period (pack.correction_assessment). Until now a task was raised by a scheduled agent's run (the worker) or an approved proposal (the executor); this
-- is a third source, `system`, raised by the worker's own platform job with no run and no proposal, in the same steps as a run's task: the worker updates it while it is open and clears it
-- when its finding clears; a person marks it done or dismisses it. Nothing else about a task changes. The widened check is added NOT VALID and validated by 001125.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.compliance_task DROP CONSTRAINT compliance_task_source;
ALTER TABLE fyniq.compliance_task ADD CONSTRAINT compliance_task_source CHECK (
  (source = 'agent_run' AND run_id IS NOT NULL AND proposal_id IS NULL)
  OR (source = 'ai_proposal' AND proposal_id IS NOT NULL)
  OR (source = 'system' AND run_id IS NULL AND proposal_id IS NULL)) NOT VALID;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.compliance_task_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A compliance task is kept: it is never deleted' USING ERRCODE = 'FY351';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'open' OR NEW.version <> 1 THEN
      RAISE EXCEPTION 'A compliance task starts open, at version 1' USING ERRCODE = 'FY351';
    END IF;
    IF NOT pg_has_role(current_user, CASE NEW.source WHEN 'ai_proposal' THEN 'fyniq_executor' ELSE 'fyniq_worker' END, 'MEMBER') THEN
      RAISE EXCEPTION 'A run''s or the platform''s task is the worker''s, a proposal''s the executor''s' USING ERRCODE = 'FY351';
    END IF;
    RETURN NEW;
  END IF;
  IF (NEW.organisation_id, NEW.id, NEW.finding_key, NEW.source, NEW.run_id, NEW.proposal_id, NEW.created_at)
     IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.finding_key, OLD.source, OLD.run_id, OLD.proposal_id, OLD.created_at) THEN
    RAISE EXCEPTION 'What a compliance task is about never changes' USING ERRCODE = 'FY351';
  END IF;
  IF OLD.status <> 'open' THEN
    RAISE EXCEPTION 'A closed compliance task stays closed: a finding that returns raises a new one' USING ERRCODE = 'FY351';
  END IF;
  IF NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'Each change of a compliance task is its next version' USING ERRCODE = 'FY351';
  END IF;
  IF NEW.status IN ('done', 'dismissed') AND NOT pg_has_role(current_user, 'fyniq_api', 'MEMBER') THEN
    RAISE EXCEPTION 'A person marks a compliance task done or dismisses it, through the API' USING ERRCODE = 'FY351';
  END IF;
  IF NEW.status IN ('open', 'cleared') AND NOT pg_has_role(current_user, 'fyniq_worker', 'MEMBER') THEN
    RAISE EXCEPTION 'Only the worker updates an open compliance task or clears it' USING ERRCODE = 'FY351';
  END IF;
  IF NEW.status <> 'open' AND (NEW.title, NEW.detail, NEW.owner_role, NEW.owner_user, NEW.due_on, NEW.escalated)
     IS DISTINCT FROM (OLD.title, OLD.detail, OLD.owner_role, OLD.owner_user, OLD.due_on, OLD.escalated) THEN
    RAISE EXCEPTION 'A compliance task closes as it was' USING ERRCODE = 'FY351';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- The worker already holds INSERT on the columns a system task uses (run_id is simply left null); the executor's grant stays a proposal's alone.
