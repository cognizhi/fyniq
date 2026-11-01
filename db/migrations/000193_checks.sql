-- The checks engine (PRD-001 FR-CTL-01, FR-CTL-02, FR-CTL-05; p1-checks task 1.3, design D4).
--
-- Each check of the catalogue (Appendix C) keeps its latest result, each run of the checks is recorded, and a person may
-- be named as a check's owner instead of the role the catalogue names. The database is the backstop:
--   - check_state: one row per organisation and check, written and replaced by the worker as the checks run; a change of
--     status is audited on the row in the same transaction (AUD-01), and a rerun that finds the same moves only when it
--     ran. A row is never deleted (FY245).
--   - check_run: each run, appended by the worker, never changed; a run is deleted only once it is past its retention,
--     90 days for a run on a change or on request and 13 months for a nightly run, whose full results are the evidence the
--     Compliance Center reads (FY245).
--   - check_owner: the person named for a check, written by the API at its next version and audited (AUD-01); a person
--     who is a member of the organisation, or nobody (the catalogue's role again). Never deleted (FY245).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.check_state (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  key             text        NOT NULL,
  status          text        NOT NULL,
  item_count      integer     NOT NULL,
  items           jsonb       NOT NULL DEFAULT '[]',
  detail          text,
  fix             text,
  needs           text,
  setting         text,
  run_at          timestamptz NOT NULL,
  run_trigger     text        NOT NULL,
  ledger_revision bigint      NOT NULL,
  changed_at      timestamptz NOT NULL,
  version         integer     NOT NULL DEFAULT 1,
  CONSTRAINT check_state_pkey PRIMARY KEY (organisation_id, key),
  CONSTRAINT check_state_id_key UNIQUE (organisation_id, id),
  CONSTRAINT check_state_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT check_state_key_format CHECK (key ~ '^[a-z][a-z0-9-]*(\.[a-z0-9-]+)+$' AND length(key) <= 100),
  CONSTRAINT check_state_status_known CHECK (status IN ('ok', 'warning', 'error', 'due', 'not_assessed', 'failed')),
  CONSTRAINT check_state_count CHECK (item_count >= 0),
  CONSTRAINT check_state_items CHECK (jsonb_typeof(items) = 'array' AND jsonb_array_length(items) <= 50
    AND jsonb_array_length(items) <= item_count AND octet_length(items::text) <= 262144),
  CONSTRAINT check_state_words CHECK (length(detail) <= 2000 AND length(fix) <= 500 AND length(needs) <= 300 AND length(setting) <= 200),
  CONSTRAINT check_state_not_assessed_names_input CHECK ((status = 'not_assessed') = (needs IS NOT NULL)),
  CONSTRAINT check_state_trigger_known CHECK (run_trigger IN ('change', 'nightly', 'request')),
  CONSTRAINT check_state_revision CHECK (ledger_revision >= 0),
  CONSTRAINT check_state_version CHECK (version >= 1)
);

-- +goose StatementBegin
-- A check's state is written at version 1 and replaced only as its next version by a later run; it is never deleted.
CREATE FUNCTION fyniq.check_state_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.version <> 1 THEN
      RAISE EXCEPTION 'A check''s state is written at version 1'
        USING ERRCODE = 'FY245', HINT = 'Run the checks; each run replaces the state as its next version (FR-CTL-01).';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP <> 'UPDATE' THEN
    RAISE EXCEPTION 'A check''s state is never deleted or truncated'
      USING ERRCODE = 'FY245', HINT = 'Run the checks again: the next run replaces it (FR-CTL-01).';
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.key IS DISTINCT FROM OLD.key OR NEW.version <> OLD.version + 1
     OR NEW.run_at < OLD.run_at OR NEW.changed_at < OLD.changed_at THEN
    RAISE EXCEPTION 'The state of check % changes only as its next version, from a later run', OLD.key
      USING ERRCODE = 'FY245', HINT = 'Read the check''s state and write the run''s result as its next version (FR-CTL-01).';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER check_state_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.check_state
  FOR EACH ROW EXECUTE FUNCTION fyniq.check_state_guard();
CREATE TRIGGER check_state_no_truncate BEFORE TRUNCATE ON fyniq.check_state
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.check_state_guard();

ALTER TABLE fyniq.check_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.check_state FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.check_state
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- A check's first result and every change of its status: each audited on the check in the same transaction (AUD-01). A
-- rerun that finds the same status moves only when it ran, and adds nothing to the trail.
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.check_state
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('check_state', 'id');
CREATE CONSTRAINT TRIGGER require_audit_status AFTER UPDATE ON fyniq.check_state
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (OLD.status IS DISTINCT FROM NEW.status)
  EXECUTE FUNCTION fyniq.require_audit('check_state', 'id');

REVOKE ALL ON fyniq.check_state FROM PUBLIC;
GRANT SELECT ON fyniq.check_state TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (key, status, item_count, items, detail, fix, needs, setting, run_at, run_trigger, ledger_revision, changed_at)
  ON fyniq.check_state TO fyniq_worker;
GRANT UPDATE (status, item_count, items, detail, fix, needs, setting, run_at, run_trigger, ledger_revision, changed_at, version)
  ON fyniq.check_state TO fyniq_worker;

CREATE TABLE fyniq.check_run (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  run_trigger     text        NOT NULL,
  requested_by    text,
  started_at      timestamptz NOT NULL,
  finished_at     timestamptz NOT NULL,
  ledger_revision bigint      NOT NULL,
  audit_seq       bigint      NOT NULL,
  keys            text[]      NOT NULL,
  results         jsonb,
  CONSTRAINT check_run_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT check_run_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT check_run_trigger_known CHECK (run_trigger IN ('change', 'nightly', 'request')),
  CONSTRAINT check_run_requested_by CHECK ((run_trigger = 'request') = (requested_by IS NOT NULL) AND length(requested_by) <= 200),
  CONSTRAINT check_run_times CHECK (finished_at >= started_at),
  CONSTRAINT check_run_watermarks CHECK (ledger_revision >= 0 AND audit_seq >= 0),
  CONSTRAINT check_run_keys CHECK (cardinality(keys) <= 500),
  CONSTRAINT check_run_nightly_results CHECK ((run_trigger = 'nightly') = (results IS NOT NULL)
    AND (results IS NULL OR (jsonb_typeof(results) = 'array' AND octet_length(results::text) <= 4194304)))
);

CREATE INDEX check_run_latest ON fyniq.check_run (organisation_id, run_trigger, finished_at DESC);

-- +goose StatementBegin
-- A run is appended and never changed. It is deleted only once past its retention: 90 days for a run on a change or on
-- request, 13 months for a nightly run (design D4).
CREATE FUNCTION fyniq.check_run_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  kept interval;
BEGIN
  IF TG_OP = 'DELETE' THEN
    kept := CASE WHEN OLD.run_trigger = 'nightly' THEN interval '13 months' ELSE interval '90 days' END;
    IF OLD.finished_at > now() - kept THEN
      RAISE EXCEPTION 'Check run % is within its retention', OLD.id
        USING ERRCODE = 'FY245', HINT = 'Runs on a change or request are kept 90 days, nightly runs 13 months (FR-CTL-05).';
    END IF;
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'A check run is never changed or truncated'
    USING ERRCODE = 'FY245', HINT = 'Each run is recorded once, as it finished (FR-CTL-01).';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER check_run_guard BEFORE UPDATE OR DELETE ON fyniq.check_run
  FOR EACH ROW EXECUTE FUNCTION fyniq.check_run_guard();
CREATE TRIGGER check_run_no_truncate BEFORE TRUNCATE ON fyniq.check_run
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.check_run_guard();

ALTER TABLE fyniq.check_run ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.check_run FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.check_run
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.check_run FROM PUBLIC;
GRANT SELECT ON fyniq.check_run TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (run_trigger, requested_by, started_at, finished_at, ledger_revision, audit_seq, keys, results), DELETE
  ON fyniq.check_run TO fyniq_worker;

CREATE TABLE fyniq.check_owner (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  key             text        NOT NULL,
  person_user_id  uuid,
  set_by          text        NOT NULL,
  set_at          timestamptz NOT NULL,
  version         integer     NOT NULL DEFAULT 1,
  CONSTRAINT check_owner_pkey PRIMARY KEY (organisation_id, key),
  CONSTRAINT check_owner_id_key UNIQUE (organisation_id, id),
  CONSTRAINT check_owner_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT check_owner_person_fkey FOREIGN KEY (organisation_id, person_user_id)
    REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT check_owner_key_format CHECK (key ~ '^[a-z][a-z0-9-]*(\.[a-z0-9-]+)+$' AND length(key) <= 100),
  CONSTRAINT check_owner_set_by CHECK (length(btrim(set_by)) BETWEEN 1 AND 200),
  CONSTRAINT check_owner_version CHECK (version >= 1)
);

CREATE INDEX check_owner_person ON fyniq.check_owner (organisation_id, person_user_id);

-- +goose StatementBegin
-- A check's owner is written at version 1 and changed only as its next version, later; never deleted.
CREATE FUNCTION fyniq.check_owner_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.version <> 1 THEN
      RAISE EXCEPTION 'A check''s owner is written at version 1'
        USING ERRCODE = 'FY245', HINT = 'Name the owner; a change is its next version (FR-CTL-01).';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP <> 'UPDATE' THEN
    RAISE EXCEPTION 'A check''s owner is never deleted or truncated'
      USING ERRCODE = 'FY245', HINT = 'Clear the person to give the check back to its role (FR-CTL-01).';
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.key IS DISTINCT FROM OLD.key OR NEW.version <> OLD.version + 1
     OR NEW.set_at < OLD.set_at THEN
    RAISE EXCEPTION 'The owner of check % changes only as its next version, set later', OLD.key
      USING ERRCODE = 'FY245', HINT = 'Read the check''s owner and name the change as its next version (FR-CTL-01).';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER check_owner_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.check_owner
  FOR EACH ROW EXECUTE FUNCTION fyniq.check_owner_guard();
CREATE TRIGGER check_owner_no_truncate BEFORE TRUNCATE ON fyniq.check_owner
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.check_owner_guard();

ALTER TABLE fyniq.check_owner ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.check_owner FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.check_owner
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Named and changed: each audited on the owner in the same transaction (AUD-01).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.check_owner
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('check_owner', 'id');

REVOKE ALL ON fyniq.check_owner FROM PUBLIC;
GRANT SELECT ON fyniq.check_owner TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (key, person_user_id, set_by, set_at) ON fyniq.check_owner TO fyniq_api;
GRANT UPDATE (person_user_id, set_by, set_at, version) ON fyniq.check_owner TO fyniq_api;
