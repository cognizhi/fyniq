-- Onboarding tasks that close by their own flow (PRD-002 NM-ONB-01, rev 0.75): a template task and the plan task copied from it may name the flow that closes it, issuing the person's key employment terms
-- or inviting them to Fyniq, so the plan never says done for what did not happen and never stays open for what did. A task that closes by its flow is not marked done by hand; the people team skips it
-- when it does not apply. What closes it is the flow's own transaction: the same one that issues the letter or marks the invitation done.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.onboarding_template_task ADD COLUMN closes text;
ALTER TABLE nemura.onboarding_template_task ADD CONSTRAINT onboarding_template_task_closes_known CHECK (closes IS NULL OR closes IN ('kets', 'fyniq_invitation')) NOT VALID;
ALTER TABLE nemura.onboarding_task ADD COLUMN closes text;
ALTER TABLE nemura.onboarding_task ADD CONSTRAINT onboarding_task_closes_known CHECK (closes IS NULL OR closes IN ('kets', 'fyniq_invitation')) NOT VALID;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.onboarding_task_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR OLD.state <> 'open' OR NEW.state = 'open'
     OR (NEW.plan_id, NEW.person_id, NEW.position, NEW.title, NEW.detail, NEW.assignee, NEW.due_on, NEW.closes) IS DISTINCT FROM (OLD.plan_id, OLD.person_id, OLD.position, OLD.title, OLD.detail, OLD.assignee, OLD.due_on, OLD.closes) THEN
    RAISE EXCEPTION 'An onboarding task is done or skipped once, and its words and due date never change' USING ERRCODE = 'NM212';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT INSERT (closes) ON nemura.onboarding_template_task TO nemura_api;
GRANT INSERT (closes) ON nemura.onboarding_task TO nemura_api;
