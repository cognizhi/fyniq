-- Hiring, offer to hire (PRD-002 NM-REC-06): a candidate at the offer stage is hired once: a person record is made with the start date, an onboarding plan is started and the requisition is filled, and the
-- Employment Pass or S Pass application date is tracked. The hire row ties the candidate to the person; the candidate's own record is final once hired.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.hire (
  organisation_id      uuid        NOT NULL DEFAULT nemura.current_org(),
  id                   uuid        NOT NULL DEFAULT uuidv7(),
  candidate_id         uuid        NOT NULL,
  requisition_id       uuid        NOT NULL,
  person_id            uuid        NOT NULL,
  plan_id              uuid,
  start_date           date        NOT NULL,
  pass_needed          boolean     NOT NULL,
  pass_application_on  date,        -- the day the pass application was made, recorded once
  hired_by             uuid        NOT NULL,
  hired_at             timestamptz NOT NULL,
  CONSTRAINT hire_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT hire_candidate_key UNIQUE (organisation_id, candidate_id),
  CONSTRAINT hire_candidate_fkey FOREIGN KEY (organisation_id, candidate_id) REFERENCES nemura.candidate (organisation_id, id),
  CONSTRAINT hire_requisition_fkey FOREIGN KEY (organisation_id, requisition_id) REFERENCES nemura.requisition (organisation_id, id),
  CONSTRAINT hire_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT hire_by_fkey FOREIGN KEY (organisation_id, hired_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT hire_pass_whole CHECK (pass_application_on IS NULL OR pass_needed)
);
-- +goose StatementBegin
CREATE FUNCTION nemura.hire_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (NEW.organisation_id, NEW.id, NEW.candidate_id, NEW.requisition_id, NEW.person_id, NEW.plan_id, NEW.start_date, NEW.pass_needed, NEW.hired_by, NEW.hired_at)
       IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.candidate_id, OLD.requisition_id, OLD.person_id, OLD.plan_id, OLD.start_date, OLD.pass_needed, OLD.hired_by, OLD.hired_at) THEN
    RAISE EXCEPTION 'A hire is never deleted or rewritten: only the pass application date is recorded, once' USING ERRCODE = 'NM216';
  END IF;
  IF OLD.pass_application_on IS NOT NULL AND NEW.pass_application_on IS DISTINCT FROM OLD.pass_application_on THEN
    RAISE EXCEPTION 'The pass application date is recorded once' USING ERRCODE = 'NM216';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER hire_guard BEFORE UPDATE OR DELETE ON nemura.hire FOR EACH ROW EXECUTE FUNCTION nemura.hire_guard();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.hire DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('candidate', 'candidate_id');

ALTER TABLE nemura.hire ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.hire FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.hire USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
CREATE POLICY hiring_scope ON nemura.hire AS RESTRICTIVE FOR ALL TO nemura_api USING (nemura.hiring_team()) WITH CHECK (nemura.hiring_team());
REVOKE ALL ON nemura.hire FROM PUBLIC;
GRANT SELECT ON nemura.hire TO nemura_api, nemura_readonly;
GRANT INSERT (id, candidate_id, requisition_id, person_id, plan_id, start_date, pass_needed, hired_by, hired_at), UPDATE (pass_application_on) ON nemura.hire TO nemura_api;
