-- The workbook archived and the migration closed (PRD-001 §17.1 M6, FR-MIG-01 rev 0.269; p1-workbook-migration task 6.1,
-- design D11). Besides its workbooks and their Checks exports, a migration keeps the PDFs of the workbook's reports, for
-- the archive. At go-live each of its files is copied write-once to the retention-locked evidence store, under the
-- records' retention (FR-DOC-07, §11.9), and each copy is recorded with the file's hash: an archive row is added only
-- while the migration is open and its parallel month signed, for a file of its own, with that file's hash, and never
-- changes or goes (FY329). The migration then closes, read only: only once every file it keeps is archived (FY325). The
-- file's kinds and media types are widened NOT VALID and validated by 000339.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.migration_file DROP CONSTRAINT migration_file_kind_known;
ALTER TABLE fyniq.migration_file ADD CONSTRAINT migration_file_kind_known CHECK (kind IN ('workbook', 'checks', 'pdf')) NOT VALID;
ALTER TABLE fyniq.migration_file DROP CONSTRAINT migration_file_media_type;
ALTER TABLE fyniq.migration_file ADD CONSTRAINT migration_file_media_type CHECK (
  media_type IN ('application/vnd.openxmlformats-officedocument.spreadsheetml.sheet', 'text/csv', 'application/pdf')) NOT VALID;

CREATE TABLE fyniq.migration_archive (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  run_id          uuid        NOT NULL,
  file_id         uuid        NOT NULL,  -- the file archived
  locked_key      text        NOT NULL,  -- its copy in the retention-locked store
  sha256          text        NOT NULL,  -- the copy's hash, the file's
  retain_until    timestamptz NOT NULL,
  archived_by     text        NOT NULL,
  archived_at     timestamptz NOT NULL,
  CONSTRAINT migration_archive_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT migration_archive_run_fkey FOREIGN KEY (organisation_id, run_id) REFERENCES fyniq.migration_run (organisation_id, id),
  CONSTRAINT migration_archive_file_fkey FOREIGN KEY (organisation_id, file_id) REFERENCES fyniq.migration_file (organisation_id, id),
  CONSTRAINT migration_archive_file UNIQUE (organisation_id, file_id),
  CONSTRAINT migration_archive_key_format CHECK (locked_key ~ '^migrations/[0-9a-f-]{36}/[0-9a-f-]{36}/[0-9a-f-]{36}$'),
  CONSTRAINT migration_archive_hash CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT migration_archive_retained CHECK (retain_until > archived_at),
  CONSTRAINT migration_archive_archived_by_format CHECK (archived_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX migration_archive_run ON fyniq.migration_archive (organisation_id, run_id);

-- +goose StatementBegin
CREATE FUNCTION fyniq.migration_archive_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A migration''s archive is kept as it was made' USING ERRCODE = 'FY329';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.migration_run r WHERE r.organisation_id = NEW.organisation_id AND r.id = NEW.run_id AND r.status = 'open') THEN
    RAISE EXCEPTION 'The migration is closed' USING ERRCODE = 'FY329';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.migration_signoff o WHERE o.organisation_id = NEW.organisation_id AND o.run_id = NEW.run_id
      AND o.subject = 'parallel') THEN
    RAISE EXCEPTION 'The workbook is archived once the parallel month is signed' USING ERRCODE = 'FY329';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.migration_file f WHERE f.organisation_id = NEW.organisation_id AND f.id = NEW.file_id
      AND f.run_id = NEW.run_id AND f.sha256 = NEW.sha256) THEN
    RAISE EXCEPTION 'An archive copy is of a file of its own migration, with its hash' USING ERRCODE = 'FY329';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER migration_archive_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.migration_archive
  FOR EACH ROW EXECUTE FUNCTION fyniq.migration_archive_guard();
CREATE TRIGGER migration_archive_no_truncate BEFORE TRUNCATE ON fyniq.migration_archive
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.migration_archive
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('migration_run', 'run_id');

ALTER TABLE fyniq.migration_archive ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.migration_archive FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.migration_archive
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.migration_archive FROM PUBLIC;
GRANT SELECT ON fyniq.migration_archive TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (run_id, file_id, locked_key, sha256, retain_until, archived_by, archived_at) ON fyniq.migration_archive TO fyniq_api;
-- The migration closes at M6.
GRANT UPDATE (status) ON fyniq.migration_run TO fyniq_api;

-- The migration's guard (FY325) with M6's close: once the parallel month is signed and each file archived.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.migration_guard() RETURNS trigger
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
    -- It closes at M6 (rev 0.269): once the parallel month is signed and every file it keeps is archived.
    IF TG_OP = 'UPDATE' AND (NOT EXISTS (SELECT 1 FROM fyniq.migration_signoff o WHERE o.organisation_id = NEW.organisation_id
        AND o.run_id = NEW.id AND o.subject = 'parallel') OR EXISTS (SELECT 1 FROM fyniq.migration_file f WHERE f.organisation_id = NEW.organisation_id
        AND f.run_id = NEW.id AND NOT EXISTS (SELECT 1 FROM fyniq.migration_archive x WHERE x.organisation_id = f.organisation_id AND x.file_id = f.id))) THEN
      RAISE EXCEPTION 'A migration closes once its parallel month is signed and each of its files archived' USING ERRCODE = 'FY325';
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
