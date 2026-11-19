-- Projects: the Project dimension's values with their attributes, members and tasks (PRD-001 FR-PRJ-01, FR-ORG-06;
-- p1-projects task 1.1, design D1).
--
-- A project is a value of the organisation's Project dimension (000218), so every line already tagged keeps working and
-- the balance service already sums by it. The project row holds what the value does not: the customer, the billing
-- currency, the dates, the status, the billing method and the budget in hours and in money. Its ID is the value's ID.
-- A project's members are the people who log time on it, and its managers (is_manager) are the members who approve that
-- time (FR-PRJ-07): naming a manager never gives a right of its own, which the API takes from the roles (design D4).
-- Tasks are what time is logged against.
--
-- Nothing here is ever deleted: a project is closed, a member or a task put out of use, as dimensions and their values
-- are (FY258), because entries and posted lines name them. Every change has its audit event in the same transaction
-- (AUD-08), under the project.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.project (
  organisation_id    uuid          NOT NULL DEFAULT fyniq.current_org(),
  id                 uuid          NOT NULL, -- the Project dimension value's ID
  customer_id        uuid,
  currency           text          NOT NULL,
  minor_units        smallint      NOT NULL,
  status             text          NOT NULL DEFAULT 'active',
  billing_method     text          NOT NULL,
  starts_on          date,
  ends_on            date,
  budget_minutes     integer,
  budget_amount      numeric(20,4),
  version            integer       NOT NULL DEFAULT 1,
  created_at         timestamptz   NOT NULL DEFAULT now(),
  changed_at         timestamptz   NOT NULL DEFAULT now(),
  CONSTRAINT project_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT project_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT project_value_fkey FOREIGN KEY (organisation_id, id) REFERENCES fyniq.dimension_value (organisation_id, id),
  CONSTRAINT project_customer_fkey FOREIGN KEY (organisation_id, customer_id) REFERENCES fyniq.contact (organisation_id, id),
  CONSTRAINT project_currency_fkey FOREIGN KEY (currency, minor_units) REFERENCES fyniq.currency (code, minor_units),
  CONSTRAINT project_status_known CHECK (status IN ('planned', 'active', 'on_hold', 'closed')),
  CONSTRAINT project_billing_known CHECK (billing_method IN ('time_and_materials', 'fixed_fee', 'retainer', 'internal')),
  CONSTRAINT project_dates_ordered CHECK (starts_on IS NULL OR ends_on IS NULL OR starts_on <= ends_on),
  CONSTRAINT project_budget_minutes CHECK (budget_minutes IS NULL OR budget_minutes BETWEEN 0 AND 100000000),
  CONSTRAINT project_budget_amount CHECK (budget_amount IS NULL
    OR (budget_amount <> 'NaN' AND budget_amount >= 0 AND budget_amount = trunc(budget_amount, minor_units))),
  CONSTRAINT project_version_positive CHECK (version >= 1)
);
CREATE INDEX project_customer_idx ON fyniq.project (organisation_id, customer_id) WHERE customer_id IS NOT NULL;

CREATE TABLE fyniq.project_member (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  project_id      uuid        NOT NULL,
  user_id         uuid        NOT NULL,
  is_manager      boolean     NOT NULL DEFAULT false,
  is_active       boolean     NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT project_member_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT project_member_person_key UNIQUE (organisation_id, project_id, user_id),
  CONSTRAINT project_member_project_fkey FOREIGN KEY (organisation_id, project_id)
    REFERENCES fyniq.project (organisation_id, id),
  CONSTRAINT project_member_user_fkey FOREIGN KEY (organisation_id, user_id)
    REFERENCES fyniq.user_membership (organisation_id, user_id)
);
CREATE INDEX project_member_user_idx ON fyniq.project_member (organisation_id, user_id) WHERE is_active;

CREATE TABLE fyniq.project_task (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  project_id      uuid        NOT NULL,
  name            text        NOT NULL,
  billable        boolean     NOT NULL DEFAULT true,
  is_active       boolean     NOT NULL DEFAULT true,
  version         integer     NOT NULL DEFAULT 1,
  created_at      timestamptz NOT NULL DEFAULT now(),
  changed_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT project_task_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT project_task_project_fkey FOREIGN KEY (organisation_id, project_id)
    REFERENCES fyniq.project (organisation_id, id),
  CONSTRAINT project_task_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 100),
  CONSTRAINT project_task_version_positive CHECK (version >= 1)
);
-- One live task of a name in a project, whatever the case.
CREATE UNIQUE INDEX project_task_live_name ON fyniq.project_task (organisation_id, project_id, lower(btrim(name))) WHERE is_active;
CREATE UNIQUE INDEX project_task_project_key ON fyniq.project_task (organisation_id, project_id, id);

-- +goose StatementBegin
-- A project is a value of the Project dimension, for a customer when it names one; it keeps its value, its currency and
-- its creation, changes one version at a time and is never deleted (FY600).
CREATE FUNCTION fyniq.project_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  kind text;
  customer boolean;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A project is never deleted: time and posted lines name it' USING ERRCODE = 'FY600', HINT = 'Close it instead';
  END IF;
  IF TG_OP = 'INSERT' THEN
    SELECT d.kind INTO kind FROM fyniq.dimension_value v
      JOIN fyniq.dimension d ON d.organisation_id = v.organisation_id AND d.id = v.dimension_id
     WHERE v.organisation_id = NEW.organisation_id AND v.id = NEW.id;
    IF kind IS DISTINCT FROM 'project' THEN
      RAISE EXCEPTION 'A project is a value of the Project dimension' USING ERRCODE = 'FY600',
        HINT = 'Add the value to the organisation''s Project dimension first (FR-DIM-01)';
    END IF;
    IF NEW.version <> 1 THEN
      RAISE EXCEPTION 'A new project starts at version 1' USING ERRCODE = 'FY600';
    END IF;
  ELSE
    IF NEW.version <> OLD.version + 1 OR (NEW.id, NEW.currency, NEW.minor_units, NEW.created_at)
       IS DISTINCT FROM (OLD.id, OLD.currency, OLD.minor_units, OLD.created_at) THEN
      RAISE EXCEPTION 'A project changes one version at a time and keeps its value, its currency and its creation' USING ERRCODE = 'FY600',
        HINT = 'Add another project for another currency';
    END IF;
  END IF;
  IF NEW.customer_id IS NOT NULL THEN
    SELECT c.is_customer INTO customer FROM fyniq.contact c WHERE c.organisation_id = NEW.organisation_id AND c.id = NEW.customer_id;
    IF NOT COALESCE(customer, false) THEN
      RAISE EXCEPTION 'A project''s customer is a contact marked as a customer' USING ERRCODE = 'FY600',
        HINT = 'Mark the contact as a customer, or choose another';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER project_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.project
  FOR EACH ROW EXECUTE FUNCTION fyniq.project_guard();

-- +goose StatementBegin
-- A member is an active member of the organisation when named, is never deleted (put out of use instead) and keeps its
-- project, its person and its creation; a task keeps its project, changes one version at a time, is never deleted, and
-- is added only to a project that is not closed (FY601).
CREATE FUNCTION fyniq.project_part_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  st text;
  who text;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A project''s member or task is never deleted: time names it' USING ERRCODE = 'FY601', HINT = 'Put it out of use instead';
  END IF;
  IF TG_TABLE_NAME = 'project_member' THEN
    IF TG_OP = 'INSERT' OR (NEW.is_active AND NOT OLD.is_active) THEN
      SELECT m.status INTO who FROM fyniq.user_membership m WHERE m.organisation_id = NEW.organisation_id AND m.user_id = NEW.user_id;
      IF who IS DISTINCT FROM 'active' THEN
        RAISE EXCEPTION 'A project''s member is an active member of the organisation' USING ERRCODE = 'FY601',
          HINT = 'Invite the person to the organisation first';
      END IF;
    END IF;
    IF TG_OP = 'UPDATE' AND (NEW.project_id, NEW.user_id, NEW.created_at) IS DISTINCT FROM (OLD.project_id, OLD.user_id, OLD.created_at) THEN
      RAISE EXCEPTION 'A project''s member keeps their project and their person' USING ERRCODE = 'FY601';
    END IF;
  ELSE
    IF TG_OP = 'INSERT' THEN
      SELECT p.status INTO st FROM fyniq.project p WHERE p.organisation_id = NEW.organisation_id AND p.id = NEW.project_id;
      IF st = 'closed' THEN
        RAISE EXCEPTION 'A closed project takes no new task' USING ERRCODE = 'FY601', HINT = 'Reopen the project first';
      END IF;
      IF NEW.version <> 1 THEN
        RAISE EXCEPTION 'A new task starts at version 1' USING ERRCODE = 'FY601';
      END IF;
    ELSIF NEW.version <> OLD.version + 1 OR (NEW.project_id, NEW.created_at) IS DISTINCT FROM (OLD.project_id, OLD.created_at) THEN
      RAISE EXCEPTION 'A task changes one version at a time and keeps its project' USING ERRCODE = 'FY601';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER project_member_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.project_member
  FOR EACH ROW EXECUTE FUNCTION fyniq.project_part_guard();
CREATE TRIGGER project_task_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.project_task
  FOR EACH ROW EXECUTE FUNCTION fyniq.project_part_guard();

ALTER TABLE fyniq.project ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.project FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.project
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.project_member ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.project_member FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.project_member
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.project_task ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.project_task FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.project_task
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- AUD-08: every change has its audit event under the project in the same transaction.
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.project
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('project', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.project_member
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('project', 'project_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.project_task
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('project', 'project_id');

GRANT SELECT ON fyniq.project, fyniq.project_member, fyniq.project_task TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (id, customer_id, currency, minor_units, status, billing_method, starts_on, ends_on,
              budget_minutes, budget_amount) ON fyniq.project TO fyniq_api;
GRANT UPDATE (customer_id, status, billing_method, starts_on, ends_on, budget_minutes, budget_amount, version, changed_at)
  ON fyniq.project TO fyniq_api;
GRANT INSERT (project_id, user_id, is_manager) ON fyniq.project_member TO fyniq_api;
GRANT UPDATE (is_manager, is_active) ON fyniq.project_member TO fyniq_api;
GRANT INSERT (project_id, name, billable) ON fyniq.project_task TO fyniq_api;
GRANT UPDATE (name, billable, is_active, version, changed_at) ON fyniq.project_task TO fyniq_api;
