-- The workbook migration's record (PRD-001 FR-MIG-01, §17.1; p1-workbook-migration task 1.5, design D3, D11): one
-- migration per organisation, which only an organisation without a posted entry starts (rev 0.254), with a step per
-- attempt at each of §17.1's steps M1 to M6 and the files each step keeps. M1's step is the workbook uploaded, hashed
-- and validated in the background: validating, then valid or invalid with its validation report, whose SHA-256 the
-- later sign-offs name. The files are kept as uploaded (the workbook) or as made (its Checks export), never changed.
-- A run, a step and a file never go; a step changes only from validating to its report, once (FY325). Every change is
-- audited on the run (AUD-01), under Appendix D's "Workbook import (migration)" row. The operation that validates is
-- the long-operation kind migration_validation, widened NOT VALID and validated by 000326.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.migration_run (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  status          text        NOT NULL DEFAULT 'open',  -- open; closed at M6
  started_by      text        NOT NULL,
  started_at      timestamptz NOT NULL,
  CONSTRAINT migration_run_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT migration_run_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT migration_run_status_known CHECK (status IN ('open', 'closed')),
  CONSTRAINT migration_run_started_by_format CHECK (started_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
-- One migration per organisation: a failed or rejected import starts again in a new organisation (rev 0.254).
CREATE UNIQUE INDEX migration_run_one ON fyniq.migration_run (organisation_id);

CREATE TABLE fyniq.migration_file (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  run_id          uuid        NOT NULL,
  kind            text        NOT NULL,  -- workbook: as uploaded; checks: the Checks export made from it
  file_name       text        NOT NULL,  -- as the uploader named it: data, never a path
  media_type      text        NOT NULL,  -- decided from the content
  size_bytes      bigint      NOT NULL,
  sha256          text        NOT NULL,  -- of the file as kept
  original_sha256 text        NOT NULL,  -- of the file as received
  storage_key     text        NOT NULL,
  data_class      text        NOT NULL,  -- the workbook holds payroll; the Checks export is general
  added_by        text        NOT NULL,
  added_at        timestamptz NOT NULL,
  CONSTRAINT migration_file_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT migration_file_run_fkey FOREIGN KEY (organisation_id, run_id) REFERENCES fyniq.migration_run (organisation_id, id),
  CONSTRAINT migration_file_storage_key UNIQUE (organisation_id, storage_key),
  CONSTRAINT migration_file_kind_known CHECK (kind IN ('workbook', 'checks')),
  CONSTRAINT migration_file_name_length CHECK (length(btrim(file_name)) BETWEEN 1 AND 255),
  CONSTRAINT migration_file_media_type CHECK (media_type IN ('application/vnd.openxmlformats-officedocument.spreadsheetml.sheet', 'text/csv')),
  CONSTRAINT migration_file_size CHECK (size_bytes BETWEEN 1 AND 26214400),
  CONSTRAINT migration_file_hashes CHECK (sha256 ~ '^[0-9a-f]{64}$' AND original_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT migration_file_key_format CHECK (storage_key ~ '^migrations/[0-9a-f-]{36}/[0-9a-f-]{36}/[0-9a-f-]{36}$'),
  CONSTRAINT migration_file_class_known CHECK (data_class IN ('general', 'payroll')),
  CONSTRAINT migration_file_added_by_format CHECK (added_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX migration_file_run ON fyniq.migration_file (organisation_id, run_id, added_at);

CREATE TABLE fyniq.migration_step (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  run_id           uuid        NOT NULL,
  step             text        NOT NULL,  -- M1 to M6 (§17.1)
  state            text        NOT NULL,  -- validating, then valid or invalid
  workbook_file_id uuid        NOT NULL,  -- the workbook the step validates
  checks_file_id   uuid,                  -- the Checks export made from it, once validated
  report           jsonb,                 -- the validation report, once validated
  report_sha256    text,
  errors           integer,
  warnings         integer,
  started_by       text        NOT NULL,
  started_at       timestamptz NOT NULL,
  validated_at     timestamptz,
  CONSTRAINT migration_step_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT migration_step_run_fkey FOREIGN KEY (organisation_id, run_id) REFERENCES fyniq.migration_run (organisation_id, id),
  CONSTRAINT migration_step_workbook_fkey FOREIGN KEY (organisation_id, workbook_file_id) REFERENCES fyniq.migration_file (organisation_id, id),
  CONSTRAINT migration_step_checks_fkey FOREIGN KEY (organisation_id, checks_file_id) REFERENCES fyniq.migration_file (organisation_id, id),
  CONSTRAINT migration_step_known CHECK (step IN ('M1', 'M2', 'M3', 'M4', 'M5', 'M6')),
  CONSTRAINT migration_step_state_known CHECK (state IN ('validating', 'valid', 'invalid')),
  -- Validating has no report; a report has its hash, its counts, its time and the Checks export; valid has no error.
  CONSTRAINT migration_step_report CHECK (
    (state = 'validating' AND report IS NULL AND report_sha256 IS NULL AND errors IS NULL AND warnings IS NULL AND validated_at IS NULL
      AND checks_file_id IS NULL)
    OR (state <> 'validating' AND report IS NOT NULL AND report_sha256 ~ '^[0-9a-f]{64}$' AND errors >= 0 AND warnings >= 0
      AND validated_at IS NOT NULL AND (state = 'invalid') = (errors > 0))),
  CONSTRAINT migration_step_report_size CHECK (report IS NULL OR pg_column_size(report) <= 4194304),
  CONSTRAINT migration_step_started_by_format CHECK (started_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX migration_step_run ON fyniq.migration_step (organisation_id, run_id, started_at);
CREATE INDEX migration_step_workbook ON fyniq.migration_step (organisation_id, workbook_file_id);
CREATE INDEX migration_step_checks ON fyniq.migration_step (organisation_id, checks_file_id) WHERE checks_file_id IS NOT NULL;
-- One validation at a time per step.
CREATE UNIQUE INDEX migration_step_one_validating ON fyniq.migration_step (organisation_id, run_id, step) WHERE state = 'validating';

-- The database is the backstop (FY325): a migration starts only in an organisation without a posted entry, and nothing
-- is ever removed; a step is added only to an open migration, and changes once, from validating to its report, its
-- other columns as they were.
-- +goose StatementBegin
CREATE FUNCTION fyniq.migration_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A migration''s record is kept' USING ERRCODE = 'FY325',
      HINT = 'Nothing of a migration is removed: a failed import starts again in a new organisation (rev 0.254).';
  END IF;
  IF TG_TABLE_NAME = 'migration_run' THEN
    IF TG_OP = 'INSERT' AND EXISTS (SELECT 1 FROM fyniq.journal_entry e WHERE e.organisation_id = NEW.organisation_id) THEN
      RAISE EXCEPTION 'The organisation has posted entries' USING ERRCODE = 'FY325',
        HINT = 'Import the workbook into a new organisation, never into live books (rev 0.254).';
    END IF;
    IF TG_OP = 'UPDATE' AND NOT (OLD.status = 'open' AND NEW.status = 'closed' AND NEW.id = OLD.id AND NEW.started_by = OLD.started_by
        AND NEW.started_at = OLD.started_at) THEN
      RAISE EXCEPTION 'A migration only closes' USING ERRCODE = 'FY325';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_TABLE_NAME = 'migration_file' THEN
    IF TG_OP = 'UPDATE' THEN
      RAISE EXCEPTION 'A migration''s file is kept as it was added' USING ERRCODE = 'FY325';
    END IF;
    RETURN NEW;
  END IF;
  -- migration_step
  IF TG_OP = 'INSERT' THEN
    IF NOT EXISTS (SELECT 1 FROM fyniq.migration_run r WHERE r.organisation_id = NEW.organisation_id AND r.id = NEW.run_id AND r.status = 'open') THEN
      RAISE EXCEPTION 'The migration is closed' USING ERRCODE = 'FY325';
    END IF;
    IF NEW.state <> 'validating' THEN
      RAISE EXCEPTION 'A step starts validating' USING ERRCODE = 'FY325';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM fyniq.migration_file f WHERE f.organisation_id = NEW.organisation_id AND f.id = NEW.workbook_file_id
        AND f.run_id = NEW.run_id AND f.kind = 'workbook') THEN
      RAISE EXCEPTION 'A step validates a workbook of its own migration' USING ERRCODE = 'FY325';
    END IF;
    RETURN NEW;
  END IF;
  IF OLD.state <> 'validating' OR NEW.state = 'validating' OR NEW.run_id <> OLD.run_id OR NEW.step <> OLD.step
      OR NEW.workbook_file_id <> OLD.workbook_file_id OR NEW.started_by <> OLD.started_by OR NEW.started_at <> OLD.started_at THEN
    RAISE EXCEPTION 'A step changes once, from validating to its report' USING ERRCODE = 'FY325';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.migration_file f WHERE f.organisation_id = NEW.organisation_id AND f.id = NEW.checks_file_id
      AND f.run_id = NEW.run_id AND f.kind = 'checks') THEN
    RAISE EXCEPTION 'A report keeps the Checks export of its own migration' USING ERRCODE = 'FY325';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER migration_run_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.migration_run
  FOR EACH ROW EXECUTE FUNCTION fyniq.migration_guard();
CREATE TRIGGER migration_file_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.migration_file
  FOR EACH ROW EXECUTE FUNCTION fyniq.migration_guard();
CREATE TRIGGER migration_step_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.migration_step
  FOR EACH ROW EXECUTE FUNCTION fyniq.migration_guard();
CREATE TRIGGER migration_run_no_truncate BEFORE TRUNCATE ON fyniq.migration_run
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER migration_file_no_truncate BEFORE TRUNCATE ON fyniq.migration_file
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER migration_step_no_truncate BEFORE TRUNCATE ON fyniq.migration_step
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.migration_run
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('migration_run', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.migration_file
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('migration_run', 'run_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.migration_step
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('migration_run', 'run_id');

ALTER TABLE fyniq.migration_run ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.migration_run FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.migration_run
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.migration_file ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.migration_file FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.migration_file
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.migration_step ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.migration_step FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.migration_step
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.migration_run, fyniq.migration_file, fyniq.migration_step FROM PUBLIC;
GRANT SELECT ON fyniq.migration_run, fyniq.migration_file, fyniq.migration_step TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (started_by, started_at) ON fyniq.migration_run TO fyniq_api;
GRANT INSERT (run_id, kind, file_name, media_type, size_bytes, sha256, original_sha256, storage_key, data_class, added_by, added_at)
  ON fyniq.migration_file TO fyniq_api, fyniq_worker;
GRANT INSERT (run_id, step, state, workbook_file_id, started_by, started_at) ON fyniq.migration_step TO fyniq_api;
GRANT UPDATE (state, checks_file_id, report, report_sha256, errors, warnings, validated_at) ON fyniq.migration_step TO fyniq_worker;

-- The long operation that validates a workbook (FR-INT-11).
ALTER TABLE fyniq.operation DROP CONSTRAINT operation_kind_known;
ALTER TABLE fyniq.operation ADD CONSTRAINT operation_kind_known
  CHECK (kind IN ('report_export', 'checks_run', 'migration_validation')) NOT VALID;

