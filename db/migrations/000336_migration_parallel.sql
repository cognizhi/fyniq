-- The parallel month and its sign-off (PRD-001 §17.1 M5, FR-MIG-02 rev 0.268; p1-workbook-migration task 6.1, design
-- D11). Once the import is committed and its parity report signed (M4), the workbook as it stands at the end of the
-- parallel month is uploaded as the step M5, with its Checks export: the commit's guard (FY327) lets that step and its
-- files join, and still no mapping version or other step. The Accountant signs the parallel month's report, over its
-- hash, as the sign-off's third subject, parallel: once its workbook is valid and the parity report signed, each report
-- once (FY326). The subject is widened NOT VALID and validated by 000337.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.migration_commit_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_TABLE_NAME IN ('migration_step', 'migration_file', 'migration_mapping') THEN
    IF EXISTS (SELECT 1 FROM fyniq.migration_commit c WHERE c.organisation_id = NEW.organisation_id AND c.run_id = NEW.run_id) THEN
      -- The parallel month's workbook (M5) and its Checks export join once the import is committed and its parity
      -- report signed (rev 0.268); the import's workbook and mapping stay fixed.
      IF TG_TABLE_NAME = 'migration_mapping' OR NOT EXISTS (SELECT 1 FROM fyniq.migration_commit c WHERE c.organisation_id = NEW.organisation_id
          AND c.run_id = NEW.run_id AND c.status = 'committed') OR NOT EXISTS (SELECT 1 FROM fyniq.migration_signoff o
          WHERE o.organisation_id = NEW.organisation_id AND o.run_id = NEW.run_id AND o.subject = 'reconciliation') THEN
        RAISE EXCEPTION 'The migration is committed: its workbook and mapping are fixed' USING ERRCODE = 'FY327',
          HINT = 'A rejected import starts again in a new organisation (rev 0.254); the parallel month''s workbook follows the signed parity report.';
      END IF;
      IF TG_TABLE_NAME = 'migration_step' THEN
        IF NEW.step <> 'M5' THEN
          RAISE EXCEPTION 'After the commit, only the parallel month''s workbook (M5) is added' USING ERRCODE = 'FY327';
        END IF;
      END IF;
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A migration''s commit is kept' USING ERRCODE = 'FY327',
      HINT = 'Posted history is reversed, never deleted: a rejected import is abandoned with its organisation.';
  END IF;
  IF TG_TABLE_NAME = 'migration_record' THEN
    IF TG_OP = 'UPDATE' THEN
      RAISE EXCEPTION 'A migration record is kept as it was imported' USING ERRCODE = 'FY327';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM fyniq.migration_commit c WHERE c.organisation_id = NEW.organisation_id AND c.id = NEW.commit_id
        AND c.run_id = NEW.run_id AND c.status = 'running') THEN
      RAISE EXCEPTION 'A record joins a running commit of its own migration' USING ERRCODE = 'FY327';
    END IF;
    RETURN NEW;
  END IF;
  -- migration_commit
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'running' THEN
      RAISE EXCEPTION 'A commit starts running' USING ERRCODE = 'FY327';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM fyniq.migration_run r WHERE r.organisation_id = NEW.organisation_id AND r.id = NEW.run_id AND r.status = 'open') THEN
      RAISE EXCEPTION 'The migration is closed' USING ERRCODE = 'FY327';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM fyniq.approval_request a WHERE a.organisation_id = NEW.organisation_id AND a.id = NEW.approval_id
        AND a.object_type = 'migration_commit' AND a.operation = 'migration.commit' AND a.object_key = NEW.run_id::text
        AND a.status = 'approved') THEN
      RAISE EXCEPTION 'A commit is recorded only under its own approved request' USING ERRCODE = 'FY327';
    END IF;
    IF NEW.step_id IS DISTINCT FROM (SELECT s.id FROM fyniq.migration_step s WHERE s.organisation_id = NEW.organisation_id AND s.run_id = NEW.run_id
        AND s.step = 'M1' ORDER BY s.started_at DESC, s.id DESC LIMIT 1)
        OR NOT EXISTS (SELECT 1 FROM fyniq.migration_step s WHERE s.organisation_id = NEW.organisation_id AND s.id = NEW.step_id AND s.state = 'valid') THEN
      RAISE EXCEPTION 'A commit imports its migration''s latest workbook, valid' USING ERRCODE = 'FY327';
    END IF;
    IF NEW.mapping_id IS DISTINCT FROM (SELECT m.id FROM fyniq.migration_mapping m WHERE m.organisation_id = NEW.organisation_id AND m.run_id = NEW.run_id
        ORDER BY m.version DESC LIMIT 1)
        OR NOT EXISTS (SELECT 1 FROM fyniq.migration_mapping m JOIN fyniq.migration_signoff o ON o.organisation_id = m.organisation_id AND o.mapping_id = m.id
          WHERE m.organisation_id = NEW.organisation_id AND m.id = NEW.mapping_id AND m.step_id = NEW.step_id) THEN
      RAISE EXCEPTION 'A commit imports by the latest mapping, of its workbook and signed' USING ERRCODE = 'FY327';
    END IF;
    RETURN NEW;
  END IF;
  IF NOT (OLD.status = 'running' AND NEW.status = 'committed' AND NEW.run_id = OLD.run_id AND NEW.approval_id = OLD.approval_id
      AND NEW.path = OLD.path AND NEW.step_id = OLD.step_id AND NEW.mapping_id = OLD.mapping_id AND NEW.records = OLD.records
      AND NEW.approved_by = OLD.approved_by AND NEW.approved_at = OLD.approved_at) THEN
    RAISE EXCEPTION 'A commit changes once, from running to committed' USING ERRCODE = 'FY327';
  END IF;
  IF (SELECT count(*) FROM fyniq.migration_record x WHERE x.organisation_id = NEW.organisation_id AND x.commit_id = NEW.id) <> NEW.records THEN
    RAISE EXCEPTION 'A commit is committed once it holds every record it planned' USING ERRCODE = 'FY327';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

ALTER TABLE fyniq.migration_signoff DROP CONSTRAINT migration_signoff_subject_known;
ALTER TABLE fyniq.migration_signoff ADD CONSTRAINT migration_signoff_subject_known CHECK (subject IN ('mapping', 'reconciliation', 'parallel')) NOT VALID;

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
    IF NEW.subject = 'parallel' THEN
      IF NOT EXISTS (SELECT 1 FROM fyniq.migration_signoff o WHERE o.organisation_id = NEW.organisation_id AND o.run_id = NEW.run_id
          AND o.subject = 'reconciliation') OR NOT EXISTS (SELECT 1 FROM fyniq.migration_step s WHERE s.organisation_id = NEW.organisation_id
          AND s.run_id = NEW.run_id AND s.step = 'M5' AND s.state = 'valid') THEN
        RAISE EXCEPTION 'The parallel month is signed once its workbook is valid and the parity report signed' USING ERRCODE = 'FY326';
      END IF;
      IF EXISTS (SELECT 1 FROM fyniq.migration_signoff o WHERE o.organisation_id = NEW.organisation_id AND o.run_id = NEW.run_id
          AND o.subject = 'parallel' AND o.subject_sha256 = NEW.subject_sha256) THEN
        RAISE EXCEPTION 'The parallel month''s report is signed already' USING ERRCODE = 'FY326';
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
