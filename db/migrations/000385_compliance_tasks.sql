-- Compliance tasks (PRD-001 AI-65; p1-scheduled-agents task 3.2, design D7): what a person must do about a finding, with
-- an owner and a due date. One task is open per finding, whoever raised it: a scheduled agent's run, from a deterministic
-- check, or an approved `sec.create_task` proposal. A repeat run updates the open task and clears it when its finding
-- clears; a person marks it done or dismisses it with a reason. A task never reopens: a finding that returns raises a new
-- one. Nothing removes a task.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.compliance_task (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  finding_key     text        NOT NULL,              -- the finding it is about, such as calendar:gst_f5:2026-09-30
  title           text        NOT NULL,
  detail          text        NOT NULL DEFAULT '',
  owner_role      text,                              -- a role, or
  owner_user      uuid,                              -- a member
  due_on          date,
  escalated       boolean     NOT NULL DEFAULT false, -- the Owner is told too, before the deadline (AI-62)
  source          text        NOT NULL,              -- agent_run or ai_proposal
  run_id          uuid,                              -- the run whose finding raised it
  proposal_id     uuid,                              -- the approved proposal that made it
  status          text        NOT NULL DEFAULT 'open', -- open, then done, dismissed or cleared
  reason          text        NOT NULL DEFAULT '',   -- why it was dismissed
  created_at      timestamptz NOT NULL,
  closed_at       timestamptz,
  closed_by       uuid,                              -- who marked it done or dismissed it
  version         integer     NOT NULL DEFAULT 1,
  CONSTRAINT compliance_task_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT compliance_task_run_fkey FOREIGN KEY (organisation_id, run_id) REFERENCES fyniq.agent_run (organisation_id, id),
  CONSTRAINT compliance_task_proposal_fkey FOREIGN KEY (organisation_id, proposal_id) REFERENCES fyniq.ai_proposal (organisation_id, id),
  CONSTRAINT compliance_task_owner_user_fkey FOREIGN KEY (owner_user) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT compliance_task_closed_by_fkey FOREIGN KEY (closed_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT compliance_task_key CHECK (finding_key ~ '^[a-z][a-z_]{1,40}:[A-Za-z0-9:._#-]{1,160}$'),
  CONSTRAINT compliance_task_words CHECK (length(btrim(title)) BETWEEN 1 AND 200 AND length(detail) <= 2000 AND length(reason) <= 500),
  CONSTRAINT compliance_task_owner CHECK (num_nonnulls(owner_role, owner_user) = 1 AND (owner_role IS NULL OR owner_role ~ '^[a-z][a-z_]{1,40}$')),
  CONSTRAINT compliance_task_source CHECK ((source = 'agent_run' AND run_id IS NOT NULL AND proposal_id IS NULL)
    OR (source = 'ai_proposal' AND proposal_id IS NOT NULL)),
  CONSTRAINT compliance_task_status CHECK (status IN ('open', 'done', 'dismissed', 'cleared')),
  CONSTRAINT compliance_task_state CHECK (
    (closed_at IS NOT NULL) = (status <> 'open')
    AND (closed_by IS NOT NULL) = (status IN ('done', 'dismissed'))
    AND (reason <> '') = (status = 'dismissed')
    AND (closed_at IS NULL OR closed_at >= created_at)),
  CONSTRAINT compliance_task_version CHECK (version >= 1)
);
-- One open task per finding (AI-65).
CREATE UNIQUE INDEX compliance_task_open ON fyniq.compliance_task (organisation_id, finding_key) WHERE status = 'open';
CREATE INDEX compliance_task_by_status ON fyniq.compliance_task (organisation_id, status, due_on);

-- The steps, and the role that takes each: the worker raises a run's task, updates it while open and clears it; the
-- executor makes an approved proposal's; the API marks one done or dismissed for a person. What a task is about, and
-- what raised it, never change; once closed it stays closed.
-- +goose StatementBegin
CREATE FUNCTION fyniq.compliance_task_guard() RETURNS trigger
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
    IF NOT pg_has_role(current_user, CASE NEW.source WHEN 'agent_run' THEN 'fyniq_worker' ELSE 'fyniq_executor' END, 'MEMBER') THEN
      RAISE EXCEPTION 'A run''s task is the worker''s, a proposal''s the executor''s' USING ERRCODE = 'FY351';
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

CREATE TRIGGER compliance_task_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.compliance_task
  FOR EACH ROW EXECUTE FUNCTION fyniq.compliance_task_guard();
CREATE TRIGGER compliance_task_no_truncate BEFORE TRUNCATE ON fyniq.compliance_task
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();

-- Every change is audited in its transaction (AUD-01).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.compliance_task
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('compliance_task', 'id');

ALTER TABLE fyniq.compliance_task ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.compliance_task FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.compliance_task
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.compliance_task FROM PUBLIC;
GRANT SELECT ON fyniq.compliance_task TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT UPDATE (status, reason, closed_at, closed_by, version) ON fyniq.compliance_task TO fyniq_api;
GRANT INSERT (finding_key, title, detail, owner_role, owner_user, due_on, escalated, source, run_id, created_at),
  UPDATE (title, detail, owner_role, owner_user, due_on, escalated, status, closed_at, version)
  ON fyniq.compliance_task TO fyniq_worker;
GRANT INSERT (finding_key, title, detail, owner_role, owner_user, due_on, source, proposal_id, created_at)
  ON fyniq.compliance_task TO fyniq_executor;
