-- The security incident register (PRD-001 SEC-32; p1-security-hardening task 12.2, design D10): an incident is opened
-- with its severity and summary as soon as it is suspected, and its response is recorded step by step, in the order of
-- docs/runbooks/incident-response.md: contained, assessed (with the breach register's breach when personal data was
-- involved, CMP-PD-05), notified, recovered and reviewed, which closes it. Incidents and their steps are only ever
-- added (FY322), each audited (AUD-01), under the "Compliance Center" row of Appendix D.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.security_incident (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  title           text        NOT NULL,
  summary         text        NOT NULL,
  severity        text        NOT NULL,  -- critical, high, medium or low
  detected_at     timestamptz NOT NULL,  -- when it was first seen
  opened_by       text        NOT NULL,
  opened_at       timestamptz NOT NULL,
  CONSTRAINT security_incident_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT security_incident_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT security_incident_texts CHECK (length(btrim(title)) BETWEEN 1 AND 200 AND length(btrim(summary)) BETWEEN 1 AND 8000),
  CONSTRAINT security_incident_severity_known CHECK (severity IN ('critical', 'high', 'medium', 'low')),
  CONSTRAINT security_incident_detected CHECK (detected_at <= opened_at),
  CONSTRAINT security_incident_opened_by_format CHECK (opened_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

CREATE TABLE fyniq.security_incident_step (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  incident_id     uuid        NOT NULL,
  step            text        NOT NULL,  -- contained, assessed, notified, recovered or reviewed
  at              timestamptz NOT NULL,
  by_actor        text        NOT NULL,
  note            text        NOT NULL,  -- what was found or done
  breach_id       uuid,                  -- assessed's: the breach register's breach, when personal data was involved
  CONSTRAINT security_incident_step_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT security_incident_step_incident_fkey FOREIGN KEY (organisation_id, incident_id) REFERENCES fyniq.security_incident (organisation_id, id),
  CONSTRAINT security_incident_step_breach_fkey FOREIGN KEY (organisation_id, breach_id) REFERENCES fyniq.pd_breach (organisation_id, id),
  CONSTRAINT security_incident_step_known CHECK (step IN ('contained', 'assessed', 'notified', 'recovered', 'reviewed')),
  CONSTRAINT security_incident_step_breach CHECK (breach_id IS NULL OR step = 'assessed'),
  CONSTRAINT security_incident_step_note_length CHECK (length(btrim(note)) BETWEEN 1 AND 8000),
  CONSTRAINT security_incident_step_by_format CHECK (by_actor ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX security_incident_step_incident ON fyniq.security_incident_step (organisation_id, incident_id, at);
CREATE INDEX security_incident_step_breach ON fyniq.security_incident_step (organisation_id, breach_id) WHERE breach_id IS NOT NULL;

-- Steps come in the runbook's order: each once; nothing after the review; notifying after assessing; recovering after
-- containing; the review after recovering. Nothing changes or goes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.security_incident_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  steps text[];
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'Incidents and their steps never change or go' USING ERRCODE = 'FY322',
      HINT = 'Add the incident''s next step (SEC-32).';
  END IF;
  IF TG_TABLE_NAME = 'security_incident_step' THEN
    SELECT coalesce(array_agg(s.step ORDER BY s.at, s.id), '{}') INTO steps FROM fyniq.security_incident_step s
      WHERE s.organisation_id = NEW.organisation_id AND s.incident_id = NEW.incident_id;
    IF 'reviewed' = ANY(steps) THEN
      RAISE EXCEPTION 'The incident is reviewed and closed' USING ERRCODE = 'FY322';
    END IF;
    IF NEW.step = ANY(steps) THEN
      RAISE EXCEPTION 'The incident was % already', NEW.step USING ERRCODE = 'FY322';
    END IF;
    IF NEW.step = 'notified' AND NOT 'assessed' = ANY(steps) THEN
      RAISE EXCEPTION 'An incident is notified after it is assessed' USING ERRCODE = 'FY322';
    END IF;
    IF NEW.step = 'recovered' AND NOT 'contained' = ANY(steps) THEN
      RAISE EXCEPTION 'An incident is recovered after it is contained' USING ERRCODE = 'FY322';
    END IF;
    IF NEW.step = 'reviewed' AND NOT 'recovered' = ANY(steps) THEN
      RAISE EXCEPTION 'An incident is reviewed after it is recovered' USING ERRCODE = 'FY322';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER security_incident_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.security_incident
  FOR EACH ROW EXECUTE FUNCTION fyniq.security_incident_guard();
CREATE TRIGGER security_incident_step_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.security_incident_step
  FOR EACH ROW EXECUTE FUNCTION fyniq.security_incident_guard();
CREATE TRIGGER security_incident_no_truncate BEFORE TRUNCATE ON fyniq.security_incident
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER security_incident_step_no_truncate BEFORE TRUNCATE ON fyniq.security_incident_step
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.security_incident
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('security_incident', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.security_incident_step
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('security_incident', 'incident_id');

ALTER TABLE fyniq.security_incident ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.security_incident FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.security_incident
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.security_incident_step ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.security_incident_step FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.security_incident_step
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (title, summary, severity, detected_at, opened_by, opened_at) ON fyniq.security_incident TO fyniq_api;
GRANT INSERT (incident_id, step, at, by_actor, note, breach_id) ON fyniq.security_incident_step TO fyniq_api;
GRANT SELECT ON fyniq.security_incident, fyniq.security_incident_step TO fyniq_api, fyniq_worker, fyniq_readonly;
