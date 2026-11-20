-- Onboarding plans (PRD-002 NM-ONB-01): templates of tasks for HR, the manager, IT and the new joiner, and a plan made from a template for a person, whose tasks have due dates worked out from the start date.
--
-- A template is a list of tasks with a due offset in days from the start date (negative before it). A plan copies the template's tasks when it is made, so changing or archiving a template never changes a plan
-- already made. A task is done or skipped once; its words, its assignee kind and its due date never change, and nothing is deleted (history is kept).

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.onboarding_template (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  name            text        NOT NULL,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  archived_at     timestamptz,
  CONSTRAINT onboarding_template_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT onboarding_template_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT onboarding_template_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 100)
);
CREATE UNIQUE INDEX onboarding_template_name_once ON nemura.onboarding_template (organisation_id, lower(name)) WHERE archived_at IS NULL;

CREATE TABLE nemura.onboarding_template_task (
  organisation_id uuid    NOT NULL DEFAULT nemura.current_org(),
  id              uuid    NOT NULL DEFAULT uuidv7(),
  template_id     uuid    NOT NULL,
  position        integer NOT NULL,
  title           text    NOT NULL,
  detail          text    NOT NULL DEFAULT '',
  assignee        text    NOT NULL,
  due_offset_days integer NOT NULL,
  CONSTRAINT onboarding_template_task_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT onboarding_template_task_template_fkey FOREIGN KEY (organisation_id, template_id) REFERENCES nemura.onboarding_template (organisation_id, id),
  CONSTRAINT onboarding_template_task_position UNIQUE (organisation_id, template_id, position),
  CONSTRAINT onboarding_template_task_title_length CHECK (length(btrim(title)) BETWEEN 1 AND 200),
  CONSTRAINT onboarding_template_task_detail_length CHECK (length(detail) <= 1000),
  CONSTRAINT onboarding_template_task_assignee_known CHECK (assignee IN ('hr', 'manager', 'it', 'joiner')),
  CONSTRAINT onboarding_template_task_offset_range CHECK (due_offset_days BETWEEN -60 AND 365)
);

CREATE TABLE nemura.onboarding_plan (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  person_id       uuid        NOT NULL,
  template_name   text        NOT NULL,
  start_date      date        NOT NULL,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  CONSTRAINT onboarding_plan_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT onboarding_plan_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT onboarding_plan_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT onboarding_plan_one_each UNIQUE (organisation_id, person_id)
);

CREATE TABLE nemura.onboarding_task (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  plan_id         uuid        NOT NULL,
  person_id       uuid        NOT NULL,
  position        integer     NOT NULL,
  title           text        NOT NULL,
  detail          text        NOT NULL DEFAULT '',
  assignee        text        NOT NULL,
  due_on          date        NOT NULL,
  state           text        NOT NULL DEFAULT 'open',
  decided_by      uuid,
  decided_at      timestamptz,
  note            text,
  CONSTRAINT onboarding_task_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT onboarding_task_plan_fkey FOREIGN KEY (organisation_id, plan_id) REFERENCES nemura.onboarding_plan (organisation_id, id),
  CONSTRAINT onboarding_task_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT onboarding_task_by_fkey FOREIGN KEY (organisation_id, decided_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT onboarding_task_title_length CHECK (length(btrim(title)) BETWEEN 1 AND 200),
  CONSTRAINT onboarding_task_detail_length CHECK (length(detail) <= 1000),
  CONSTRAINT onboarding_task_assignee_known CHECK (assignee IN ('hr', 'manager', 'it', 'joiner')),
  CONSTRAINT onboarding_task_state_known CHECK (state IN ('open', 'done', 'skipped')),
  CONSTRAINT onboarding_task_decided_whole CHECK ((state = 'open') = (decided_at IS NULL AND decided_by IS NULL)),
  CONSTRAINT onboarding_task_note_length CHECK (note IS NULL OR length(note) <= 300)
);
CREATE INDEX onboarding_task_plan_idx ON nemura.onboarding_task (organisation_id, plan_id, position);
CREATE INDEX onboarding_task_open_idx ON nemura.onboarding_task (organisation_id, due_on) WHERE state = 'open';

-- A task is decided once; its words, assignee, due date and plan never change; nothing is deleted. A plan and a template's tasks are never changed or deleted either; a template is archived.
-- +goose StatementBegin
CREATE FUNCTION nemura.onboarding_task_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR OLD.state <> 'open' OR NEW.state = 'open'
     OR (NEW.plan_id, NEW.person_id, NEW.position, NEW.title, NEW.detail, NEW.assignee, NEW.due_on) IS DISTINCT FROM (OLD.plan_id, OLD.person_id, OLD.position, OLD.title, OLD.detail, OLD.assignee, OLD.due_on) THEN
    RAISE EXCEPTION 'An onboarding task is done or skipped once, and its words and due date never change' USING ERRCODE = 'NM212';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER onboarding_task_guard BEFORE UPDATE OR DELETE ON nemura.onboarding_task FOR EACH ROW EXECUTE FUNCTION nemura.onboarding_task_guard();
CREATE TRIGGER onboarding_plan_append_only BEFORE UPDATE OR DELETE ON nemura.onboarding_plan FOR EACH ROW EXECUTE FUNCTION nemura.append_only_guard('An onboarding plan');
CREATE TRIGGER onboarding_template_task_append_only BEFORE UPDATE OR DELETE ON nemura.onboarding_template_task FOR EACH ROW EXECUTE FUNCTION nemura.append_only_guard('An onboarding template task');
-- +goose StatementBegin
CREATE FUNCTION nemura.onboarding_template_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR OLD.archived_at IS NOT NULL OR NEW.archived_at IS NULL
     OR (NEW.name, NEW.created_by, NEW.created_at) IS DISTINCT FROM (OLD.name, OLD.created_by, OLD.created_at) THEN
    RAISE EXCEPTION 'An onboarding template is archived once and otherwise never changes' USING ERRCODE = 'NM212';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER onboarding_template_guard BEFORE UPDATE OR DELETE ON nemura.onboarding_template FOR EACH ROW EXECUTE FUNCTION nemura.onboarding_template_guard();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.onboarding_template DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('onboarding_template', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.onboarding_plan DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('onboarding_plan', 'id');
-- a plan's tasks are audited with the plan that makes them (one event for the plan, with their number); a task added later and a task decided each have their own event
CREATE CONSTRAINT TRIGGER require_audit AFTER UPDATE ON nemura.onboarding_task DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('onboarding_task', 'id');

ALTER TABLE nemura.onboarding_template ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.onboarding_template FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.onboarding_template USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.onboarding_template_task ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.onboarding_template_task FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.onboarding_template_task USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.onboarding_plan ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.onboarding_plan FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.onboarding_plan USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.onboarding_task ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.onboarding_task FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.onboarding_task USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

REVOKE ALL ON nemura.onboarding_template, nemura.onboarding_template_task, nemura.onboarding_plan, nemura.onboarding_task FROM PUBLIC;
GRANT SELECT ON nemura.onboarding_template, nemura.onboarding_template_task, nemura.onboarding_plan, nemura.onboarding_task TO nemura_api, nemura_readonly;
GRANT INSERT (id, name, created_by, created_at), UPDATE (archived_at) ON nemura.onboarding_template TO nemura_api;
GRANT INSERT (id, template_id, position, title, detail, assignee, due_offset_days) ON nemura.onboarding_template_task TO nemura_api;
GRANT INSERT (id, person_id, template_name, start_date, created_by, created_at) ON nemura.onboarding_plan TO nemura_api;
GRANT INSERT (id, plan_id, person_id, position, title, detail, assignee, due_on), UPDATE (state, decided_by, decided_at, note) ON nemura.onboarding_task TO nemura_api;
