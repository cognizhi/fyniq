-- The workbook migration's difference register and the sign-off of the parity report (PRD-001 §17.1 M4, FR-MIG-02
-- rev 0.266; p1-workbook-migration task 5.1, design D8). The parity report is worked out on every read from the books
-- and the kept workbook; what is kept is the Accountant's decision on each line that differs: its class (a, a cent
-- allocation; b, a deliberate rule change; c, a workbook defect), a note, and the two figures it was decided on, so a
-- decision on figures that have since moved no longer counts. A decision is made, and made again, only once the import
-- is committed and while the migration is open; nothing goes. The report and the register are signed together, over the
-- report's hash, as the migration's second sign-off subject. Every change is audited on the run (AUD-01). The sign-off's
-- subject is widened NOT VALID and validated by 000333.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.migration_difference (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  run_id          uuid        NOT NULL,
  line_key        text        NOT NULL,  -- the register line: account.month.figure, account.opening, or check.number
  class           text        NOT NULL,  -- a, b or c (FR-MIG-02)
  note            text        NOT NULL DEFAULT '',
  fyniq_figure    text        NOT NULL,  -- what Fyniq's books gave when decided: an amount, or a check's statuses
  workbook_figure text        NOT NULL,  -- the workbook's, as the report gave it
  version         integer     NOT NULL DEFAULT 1,
  decided_by      text        NOT NULL,
  decided_at      timestamptz NOT NULL,
  CONSTRAINT migration_difference_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT migration_difference_run_fkey FOREIGN KEY (organisation_id, run_id) REFERENCES fyniq.migration_run (organisation_id, id),
  CONSTRAINT migration_difference_line UNIQUE (organisation_id, run_id, line_key),
  CONSTRAINT migration_difference_line_format CHECK (line_key ~ '^([0-9A-Za-z_-]{1,20}\.(opening|[0-9]{4}-(0[1-9]|1[0-2])\.(movement|closing))|check\.[0-9]{1,3})$'),
  CONSTRAINT migration_difference_class_known CHECK (class IN ('a', 'b', 'c')),
  -- A workbook defect says what it is.
  CONSTRAINT migration_difference_note CHECK (length(note) <= 500 AND (class <> 'c' OR length(btrim(note)) > 0)),
  CONSTRAINT migration_difference_figures CHECK (length(fyniq_figure) BETWEEN 1 AND 300 AND length(workbook_figure) BETWEEN 1 AND 300),
  CONSTRAINT migration_difference_version CHECK (version >= 1),
  CONSTRAINT migration_difference_decided_by_format CHECK (decided_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- The database is the backstop (FY328): a line is decided only once its migration's import is committed and while the
-- migration is open; deciding it again keeps its line and takes the next version; nothing goes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.migration_difference_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A decision of the difference register is kept' USING ERRCODE = 'FY328',
      HINT = 'Decide the line again: its next version replaces it.';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.migration_run r WHERE r.organisation_id = NEW.organisation_id AND r.id = NEW.run_id AND r.status = 'open') THEN
    RAISE EXCEPTION 'The migration is closed' USING ERRCODE = 'FY328';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.migration_commit c WHERE c.organisation_id = NEW.organisation_id AND c.run_id = NEW.run_id
      AND c.status = 'committed') THEN
    RAISE EXCEPTION 'A difference is decided once the import is committed' USING ERRCODE = 'FY328';
  END IF;
  IF TG_OP = 'UPDATE' AND (NEW.run_id <> OLD.run_id OR NEW.line_key <> OLD.line_key OR NEW.version <> OLD.version + 1) THEN
    RAISE EXCEPTION 'Deciding a line again keeps its line and takes its next version' USING ERRCODE = 'FY328';
  END IF;
  IF TG_OP = 'INSERT' AND NEW.version <> 1 THEN
    RAISE EXCEPTION 'A line''s first decision is its version 1' USING ERRCODE = 'FY328';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER migration_difference_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.migration_difference
  FOR EACH ROW EXECUTE FUNCTION fyniq.migration_difference_guard();
CREATE TRIGGER migration_difference_no_truncate BEFORE TRUNCATE ON fyniq.migration_difference
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.migration_difference
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('migration_run', 'run_id');

ALTER TABLE fyniq.migration_difference ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.migration_difference FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.migration_difference
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.migration_difference FROM PUBLIC;
GRANT SELECT ON fyniq.migration_difference TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (run_id, line_key, class, note, fyniq_figure, workbook_figure, decided_by, decided_at) ON fyniq.migration_difference TO fyniq_api;
GRANT UPDATE (class, note, fyniq_figure, workbook_figure, version, decided_by, decided_at) ON fyniq.migration_difference TO fyniq_api;

-- The parity report and the register, signed together over the report's hash once the import is committed (M4).
ALTER TABLE fyniq.migration_signoff DROP CONSTRAINT migration_signoff_subject_known;
ALTER TABLE fyniq.migration_signoff ADD CONSTRAINT migration_signoff_subject_known CHECK (subject IN ('mapping', 'reconciliation')) NOT VALID;

-- The sign-off's guard (FY326) with the second subject: a mapping's sign-off signs its latest version over its hash, a
-- reconciliation's a committed import's report, each report once; neither changes or goes.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.migration_mapping_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  latest fyniq.migration_mapping%ROWTYPE;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A migration''s mapping and its sign-offs are kept as they were' USING ERRCODE = 'FY326',
      HINT = 'Change the mapping as its next version, and sign that.';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.migration_run r WHERE r.organisation_id = NEW.organisation_id AND r.id = NEW.run_id AND r.status = 'open') THEN
    RAISE EXCEPTION 'The migration is closed' USING ERRCODE = 'FY326';
  END IF;
  -- The mapping's rows have no subject: the sign-off's is read only on a sign-off.
  IF TG_TABLE_NAME = 'migration_signoff' THEN
    IF NEW.subject = 'reconciliation' THEN
      IF NOT EXISTS (SELECT 1 FROM fyniq.migration_commit c WHERE c.organisation_id = NEW.organisation_id AND c.run_id = NEW.run_id
          AND c.status = 'committed') THEN
        RAISE EXCEPTION 'The parity report is signed once the import is committed' USING ERRCODE = 'FY326';
      END IF;
      IF EXISTS (SELECT 1 FROM fyniq.migration_signoff o WHERE o.organisation_id = NEW.organisation_id AND o.run_id = NEW.run_id
          AND o.subject = 'reconciliation' AND o.subject_sha256 = NEW.subject_sha256) THEN
        RAISE EXCEPTION 'The report is signed already' USING ERRCODE = 'FY326';
      END IF;
      RETURN NEW;
    END IF;
  END IF;
  SELECT * INTO latest FROM fyniq.migration_mapping m WHERE m.organisation_id = NEW.organisation_id AND m.run_id = NEW.run_id
    ORDER BY m.version DESC LIMIT 1;
  IF TG_TABLE_NAME = 'migration_mapping' THEN
    IF NEW.version <> coalesce(latest.version, 0) + 1 THEN
      RAISE EXCEPTION 'The mapping''s next version is %', coalesce(latest.version, 0) + 1 USING ERRCODE = 'FY326';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM fyniq.migration_step s WHERE s.organisation_id = NEW.organisation_id AND s.id = NEW.step_id
        AND s.run_id = NEW.run_id AND s.step = 'M1' AND s.state = 'valid') THEN
      RAISE EXCEPTION 'A mapping maps a valid workbook of its own migration' USING ERRCODE = 'FY326';
    END IF;
    RETURN NEW;
  END IF;
  IF latest.id IS NULL OR NEW.mapping_id <> latest.id OR NEW.subject_sha256 <> latest.mapping_sha256 THEN
    RAISE EXCEPTION 'A sign-off signs the mapping''s latest version, over its hash' USING ERRCODE = 'FY326';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
