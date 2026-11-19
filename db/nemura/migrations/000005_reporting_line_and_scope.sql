-- The reporting line and the scope functions row-level policies use (PRD-002 NM-ROLE-03, NM-SEC-02).
--
-- Line managers come from the reporting line, effective-dated: a manager sees and acts on their reports only, and never on pay.
-- The people record (p1-nemura-records-packs, NM-PPL-01) edits this table; here it is the source of the manager scope. The acting
-- person is set per transaction beside the organisation (app.person_id), so a policy can ask "is this row mine, or my report's?"
-- and a missed check in the API cannot read what the role may not (golden rule 2).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.reporting_line (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  report_id       uuid        NOT NULL,
  manager_id      uuid        NOT NULL,
  effective_from  date        NOT NULL,
  effective_to    date,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT reporting_line_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT reporting_line_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT reporting_line_report_fkey FOREIGN KEY (organisation_id, report_id)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT reporting_line_manager_fkey FOREIGN KEY (organisation_id, manager_id)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT reporting_line_dates CHECK (effective_to IS NULL OR effective_to > effective_from),
  CONSTRAINT reporting_line_not_self CHECK (report_id <> manager_id),
  CONSTRAINT reporting_line_once UNIQUE (organisation_id, report_id, manager_id, effective_from)
);
CREATE INDEX reporting_line_manager_idx ON nemura.reporting_line (organisation_id, manager_id, effective_from);

-- A line is history: only its end date may be set, once, and nothing is deleted.
-- +goose StatementBegin
CREATE FUNCTION nemura.reporting_line_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.report_id IS DISTINCT FROM OLD.report_id OR NEW.manager_id IS DISTINCT FROM OLD.manager_id
     OR NEW.effective_from IS DISTINCT FROM OLD.effective_from OR (OLD.effective_to IS NOT NULL AND NEW.effective_to IS DISTINCT FROM OLD.effective_to) THEN
    RAISE EXCEPTION 'A reporting line is history: it can only be ended, once' USING ERRCODE = 'NM081';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER reporting_line_guard BEFORE UPDATE OR DELETE ON nemura.reporting_line
  FOR EACH ROW EXECUTE FUNCTION nemura.reporting_line_guard();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.reporting_line
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('reporting_line', 'id');

ALTER TABLE nemura.reporting_line ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.reporting_line FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.reporting_line
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
GRANT SELECT ON nemura.reporting_line TO nemura_api, nemura_worker, nemura_executor, nemura_readonly;
GRANT INSERT (id, report_id, manager_id, effective_from), UPDATE (effective_to) ON nemura.reporting_line TO nemura_api;

-- ------------------------------------------------------------------ scope functions for policies
-- The acting person of this transaction, set by the unit of work beside the organisation: set_config('app.person_id', <id>, true).
-- Unset it is NULL and is nobody: a policy that asks "is it mine?" then matches nothing.
-- +goose StatementBegin
CREATE FUNCTION nemura.current_person() RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE
AS $$
  SELECT CAST(nullif(pg_catalog.current_setting('app.person_id', true), '') AS pg_catalog.uuid)
$$;
-- +goose StatementEnd

-- Whether the acting person is the manager of report today: the scope of a line manager (NM-ROLE-03). Dated by the database's day
-- unless the transaction names one (app.on), so a test and a back-dated check agree with the business date.
-- +goose StatementBegin
CREATE FUNCTION nemura.is_manager_of(report uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT EXISTS (
    SELECT 1 FROM nemura.reporting_line l
     WHERE l.organisation_id = nemura.current_org() AND l.report_id = report AND l.manager_id = nemura.current_person()
       AND l.effective_from <= coalesce(nullif(pg_catalog.current_setting('app.on', true), '')::date, current_date)
       AND (l.effective_to IS NULL OR l.effective_to > coalesce(nullif(pg_catalog.current_setting('app.on', true), '')::date, current_date)))
$$;
-- +goose StatementEnd

-- Whether the acting person holds a role today (as the transaction's day): for policies that name a role.
-- +goose StatementBegin
CREATE FUNCTION nemura.has_role(wanted text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT EXISTS (
    SELECT 1 FROM nemura.role_assignment a
     WHERE a.organisation_id = nemura.current_org() AND a.member_id = nemura.current_person() AND a.role = wanted
       AND a.effective_from <= coalesce(nullif(pg_catalog.current_setting('app.on', true), '')::date, current_date)
       AND (a.effective_to IS NULL OR a.effective_to > coalesce(nullif(pg_catalog.current_setting('app.on', true), '')::date, current_date)))
$$;
-- +goose StatementEnd

GRANT EXECUTE ON FUNCTION nemura.current_person(), nemura.is_manager_of(uuid), nemura.has_role(text)
  TO nemura_api, nemura_worker, nemura_executor, nemura_assistant, nemura_readonly;
