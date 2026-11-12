-- Each control's status for an organisation (PRD-001 CMP-01, §14.1; p1-compliance-center slice 4, design D1 and D5).
--
-- After each organisation's verifications, compliance.run works out every control's status from its checks (pass, fail,
-- overdue, not applicable, or not assessed) and keeps it here, one row per organisation and control, written by the
-- worker. A change of status is audited on the row in the same transaction (AUD-01): control.failed when it turns to
-- fail or overdue, control.recovered when it leaves them, control.status_changed otherwise. failing_since is when it last
-- turned to fail or overdue, and is set exactly while it is; a notice to the control's owner is sent once per failure,
-- keyed by it. A row is never deleted (FY308).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.control_state (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  control         text        NOT NULL,
  status          text        NOT NULL,
  detail          text        NOT NULL,
  checks          jsonb       NOT NULL DEFAULT '[]',  -- each check's status and words, as the run worked them out
  failing_since   timestamptz,
  run_at          timestamptz NOT NULL,
  changed_at      timestamptz NOT NULL,
  version         integer     NOT NULL DEFAULT 1,
  CONSTRAINT control_state_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT control_state_one UNIQUE (organisation_id, control),
  CONSTRAINT control_state_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT control_state_control_format CHECK (control ~ '^CTL-[A-Z]+-[0-9]{2}$'),
  CONSTRAINT control_state_status_known CHECK (status IN ('pass', 'fail', 'overdue', 'not_applicable', 'not_assessed')),
  CONSTRAINT control_state_failing CHECK ((status IN ('fail', 'overdue')) = (failing_since IS NOT NULL)),
  CONSTRAINT control_state_detail_length CHECK (length(btrim(detail)) BETWEEN 1 AND 2000),
  CONSTRAINT control_state_checks_array CHECK (jsonb_typeof(checks) = 'array' AND jsonb_array_length(checks) <= 20),
  CONSTRAINT control_state_times CHECK (changed_at <= run_at AND (failing_since IS NULL OR failing_since <= run_at)),
  CONSTRAINT control_state_version_positive CHECK (version > 0)
);

-- A state is written at version 1 and changed only as its next version, by the worker; never deleted.
-- +goose StatementBegin
CREATE FUNCTION fyniq.control_state_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' AND NEW.version <> 1 THEN
    RAISE EXCEPTION 'A control''s state starts at version 1' USING ERRCODE = 'FY308';
  ELSIF TG_OP = 'UPDATE' AND (NEW.version <> OLD.version + 1 OR NEW.control <> OLD.control OR NEW.id <> OLD.id) THEN
    RAISE EXCEPTION 'A control''s state changes only as its next version' USING ERRCODE = 'FY308';
  ELSIF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A control''s state never goes' USING ERRCODE = 'FY308';
  END IF;
  RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER control_state_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.control_state
  FOR EACH ROW EXECUTE FUNCTION fyniq.control_state_guard();
CREATE TRIGGER control_state_no_truncate BEFORE TRUNCATE ON fyniq.control_state
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.control_state
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('control_state', 'id');
CREATE CONSTRAINT TRIGGER require_audit_status AFTER UPDATE ON fyniq.control_state
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (OLD.status IS DISTINCT FROM NEW.status)
  EXECUTE FUNCTION fyniq.require_audit('control_state', 'id');

ALTER TABLE fyniq.control_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.control_state FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.control_state
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (control, status, detail, checks, failing_since, run_at, changed_at) ON fyniq.control_state TO fyniq_worker;
GRANT UPDATE (status, detail, checks, failing_since, run_at, changed_at, version) ON fyniq.control_state TO fyniq_worker;
GRANT SELECT ON fyniq.control_state TO fyniq_api, fyniq_worker, fyniq_readonly;
