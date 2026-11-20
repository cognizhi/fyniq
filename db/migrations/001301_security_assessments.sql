-- The register of security assessments (PRD-001 SEC-25, CTL-CHG-12; rev 0.617; p2-security-certification slice 4, design D4).
--
-- Each penetration test or other assessment of the platform: who tested and whether they are independent, the scope, the dates,
-- and the report's SHA-256 and where it is kept (the report itself stays outside Fyniq). Each finding has its severity and the
-- day it is due (CTL-RES-04's SLA for Critical and High: 7 and 30 days; Medium 90 and Low 180); the steps it takes (fixed,
-- retested, or accepted by an Owner with a reason) are rows of their own, so nothing is ever updated: assessments, findings and
-- steps are only ever added (FY362), each audited in the same transaction (AUD-01), under the "Security assessments" row of
-- Appendix D.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.security_assessment (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  kind            text        NOT NULL,  -- penetration_test, vulnerability_assessment or other
  tester          text        NOT NULL,  -- the firm or the person
  independent     boolean     NOT NULL,  -- not the people who build and run Fyniq
  scope           text        NOT NULL,
  started_on      date        NOT NULL,
  ended_on        date        NOT NULL,
  summary         text        NOT NULL,
  report_sha256   text,                  -- the report's SHA-256, so it is shown to be the one that was received
  report_location text,                  -- where the report is kept
  recorded_by     text        NOT NULL,
  recorded_at     timestamptz NOT NULL,
  CONSTRAINT security_assessment_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT security_assessment_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT security_assessment_kind_known CHECK (kind IN ('penetration_test', 'vulnerability_assessment', 'other')),
  CONSTRAINT security_assessment_texts CHECK (length(btrim(tester)) BETWEEN 1 AND 300 AND length(btrim(scope)) BETWEEN 1 AND 2000
    AND length(btrim(summary)) BETWEEN 1 AND 4000 AND length(report_location) BETWEEN 1 AND 500),
  CONSTRAINT security_assessment_dates CHECK (ended_on >= started_on),
  CONSTRAINT security_assessment_report CHECK ((report_sha256 IS NULL OR report_sha256 ~ '^[0-9a-f]{64}$')),
  CONSTRAINT security_assessment_by_format CHECK (recorded_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

CREATE TABLE fyniq.security_finding (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  assessment_id   uuid        NOT NULL,
  ref             text        NOT NULL,  -- the tester's reference for it
  title           text        NOT NULL,
  severity        text        NOT NULL,
  found_on        date        NOT NULL,
  due_on          date,                  -- by severity (Critical 7 days, High 30, Medium 90, Low 180); none for an informational one
  recorded_by     text        NOT NULL,
  recorded_at     timestamptz NOT NULL,
  CONSTRAINT security_finding_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT security_finding_ref_once UNIQUE (organisation_id, assessment_id, ref),
  CONSTRAINT security_finding_assessment_fkey FOREIGN KEY (organisation_id, assessment_id) REFERENCES fyniq.security_assessment (organisation_id, id),
  CONSTRAINT security_finding_severity_known CHECK (severity IN ('critical', 'high', 'medium', 'low', 'info')),
  CONSTRAINT security_finding_texts CHECK (length(btrim(ref)) BETWEEN 1 AND 40 AND length(btrim(title)) BETWEEN 1 AND 300),
  CONSTRAINT security_finding_due CHECK ((severity = 'info' AND due_on IS NULL) OR (severity <> 'info' AND due_on IS NOT NULL AND due_on >= found_on)),
  CONSTRAINT security_finding_by_format CHECK (recorded_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

CREATE TABLE fyniq.security_finding_step (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  finding_id      uuid        NOT NULL,
  step            text        NOT NULL,  -- fixed, retested or accepted
  on_date         date        NOT NULL,
  reason          text,                  -- an acceptance's reason
  recorded_by     text        NOT NULL,
  recorded_at     timestamptz NOT NULL,
  CONSTRAINT security_finding_step_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT security_finding_step_once UNIQUE (organisation_id, finding_id, step),
  CONSTRAINT security_finding_step_finding_fkey FOREIGN KEY (organisation_id, finding_id) REFERENCES fyniq.security_finding (organisation_id, id),
  CONSTRAINT security_finding_step_known CHECK (step IN ('fixed', 'retested', 'accepted')),
  CONSTRAINT security_finding_step_reason CHECK ((step = 'accepted' AND length(btrim(reason)) BETWEEN 3 AND 500) OR (step <> 'accepted' AND reason IS NULL)),
  CONSTRAINT security_finding_step_by_format CHECK (recorded_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- Nothing of the register changes or goes. A step follows the finding's last: fixed first, then retested; or accepted, once,
-- before the fix is retested; none dated before the finding was found or before the step before it.
-- +goose StatementBegin
CREATE FUNCTION fyniq.security_register_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  f    fyniq.security_finding;
  last fyniq.security_finding_step;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The register of security assessments is only ever added to' USING ERRCODE = 'FY362',
      HINT = 'Record the next step of a finding, or a new assessment (SEC-25).';
  END IF;
  IF TG_TABLE_NAME <> 'security_finding_step' THEN
    RETURN NEW;
  END IF;
  SELECT * INTO f FROM fyniq.security_finding x WHERE x.organisation_id = NEW.organisation_id AND x.id = NEW.finding_id;
  SELECT * INTO last FROM fyniq.security_finding_step s WHERE s.organisation_id = NEW.organisation_id AND s.finding_id = NEW.finding_id
    ORDER BY s.recorded_at DESC, s.id DESC LIMIT 1;
  IF NEW.on_date < f.found_on OR (last.id IS NOT NULL AND NEW.on_date < last.on_date) THEN
    RAISE EXCEPTION 'A step is dated no earlier than the finding or the step before it' USING ERRCODE = 'FY362';
  END IF;
  IF (NEW.step = 'fixed' AND last.id IS NOT NULL)
     OR (NEW.step = 'retested' AND last.step IS DISTINCT FROM 'fixed')
     OR (NEW.step = 'accepted' AND last.step IS NOT DISTINCT FROM 'retested')
     OR (last.step = 'accepted') THEN
    RAISE EXCEPTION 'A finding is fixed, then retested, or accepted before it is retested: not % after %', NEW.step, coalesce(last.step, 'nothing')
      USING ERRCODE = 'FY362', HINT = 'Record the step that follows the finding''s last (SEC-25).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER security_assessment_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.security_assessment
  FOR EACH ROW EXECUTE FUNCTION fyniq.security_register_guard();
CREATE TRIGGER security_finding_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.security_finding
  FOR EACH ROW EXECUTE FUNCTION fyniq.security_register_guard();
CREATE TRIGGER security_finding_step_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.security_finding_step
  FOR EACH ROW EXECUTE FUNCTION fyniq.security_register_guard();
CREATE TRIGGER security_assessment_no_truncate BEFORE TRUNCATE ON fyniq.security_assessment
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER security_finding_no_truncate BEFORE TRUNCATE ON fyniq.security_finding
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER security_finding_step_no_truncate BEFORE TRUNCATE ON fyniq.security_finding_step
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.security_assessment
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('security_assessment', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.security_finding
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('security_finding', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.security_finding_step
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('security_finding', 'finding_id');

ALTER TABLE fyniq.security_assessment ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.security_assessment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.security_assessment
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.security_finding ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.security_finding FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.security_finding
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.security_finding_step ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.security_finding_step FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.security_finding_step
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (kind, tester, independent, scope, started_on, ended_on, summary, report_sha256, report_location, recorded_by, recorded_at)
  ON fyniq.security_assessment TO fyniq_api;
GRANT INSERT (assessment_id, ref, title, severity, found_on, due_on, recorded_by, recorded_at) ON fyniq.security_finding TO fyniq_api;
GRANT INSERT (finding_id, step, on_date, reason, recorded_by, recorded_at) ON fyniq.security_finding_step TO fyniq_api;
GRANT SELECT ON fyniq.security_assessment, fyniq.security_finding, fyniq.security_finding_step TO fyniq_api, fyniq_worker, fyniq_readonly;
