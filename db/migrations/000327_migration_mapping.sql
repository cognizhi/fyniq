-- The workbook migration's mapping and its sign-off (PRD-001 §17.1 M2, FR-MIG-01; p1-workbook-migration task 2.1, design
-- D4; rev 0.256). The mapping is kept as versions: the validation proposes one from each valid workbook, keeping the
-- choices of the one before, and a person's change makes the next. Each version names the M1 step whose workbook it
-- maps and its SHA-256. The Accountant signs the latest version over its SHA-256; a later version needs a sign-off of
-- its own. Versions and sign-offs are only ever added, in order, on an open migration (FY326), each audited on the run
-- (AUD-01), under Appendix D's "Workbook import (migration)" and "Workbook import sign-offs" rows.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.migration_mapping (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  run_id          uuid        NOT NULL,
  version         integer     NOT NULL,
  step_id         uuid        NOT NULL,  -- the M1 step whose workbook it maps
  mapping         jsonb       NOT NULL,
  mapping_sha256  text        NOT NULL,
  made_by         text        NOT NULL,
  made_at         timestamptz NOT NULL,
  CONSTRAINT migration_mapping_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT migration_mapping_run_fkey FOREIGN KEY (organisation_id, run_id) REFERENCES fyniq.migration_run (organisation_id, id),
  CONSTRAINT migration_mapping_step_fkey FOREIGN KEY (organisation_id, step_id) REFERENCES fyniq.migration_step (organisation_id, id),
  CONSTRAINT migration_mapping_version_key UNIQUE (organisation_id, run_id, version),
  CONSTRAINT migration_mapping_version_positive CHECK (version >= 1),
  CONSTRAINT migration_mapping_hash CHECK (mapping_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT migration_mapping_size CHECK (pg_column_size(mapping) <= 4194304),
  CONSTRAINT migration_mapping_made_by_format CHECK (made_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX migration_mapping_step ON fyniq.migration_mapping (organisation_id, step_id);

CREATE TABLE fyniq.migration_signoff (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  run_id          uuid        NOT NULL,
  subject         text        NOT NULL,  -- mapping (M2); the parity report and the parallel month follow
  mapping_id      uuid,                  -- the mapping version signed
  subject_sha256  text        NOT NULL,  -- the SHA-256 of what is signed
  signed_by       text        NOT NULL,
  signed_at       timestamptz NOT NULL,
  CONSTRAINT migration_signoff_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT migration_signoff_run_fkey FOREIGN KEY (organisation_id, run_id) REFERENCES fyniq.migration_run (organisation_id, id),
  CONSTRAINT migration_signoff_mapping_fkey FOREIGN KEY (organisation_id, mapping_id) REFERENCES fyniq.migration_mapping (organisation_id, id),
  CONSTRAINT migration_signoff_subject_known CHECK (subject IN ('mapping')),
  CONSTRAINT migration_signoff_subject_names CHECK ((subject = 'mapping') = (mapping_id IS NOT NULL)),
  CONSTRAINT migration_signoff_hash CHECK (subject_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT migration_signoff_signed_by_format CHECK (signed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX migration_signoff_run ON fyniq.migration_signoff (organisation_id, run_id, signed_at);
-- A version is signed once.
CREATE UNIQUE INDEX migration_signoff_mapping ON fyniq.migration_signoff (organisation_id, mapping_id) WHERE mapping_id IS NOT NULL;

-- The database is the backstop (FY326): a version follows the one before, maps a valid workbook of its own open
-- migration and never changes; a sign-off signs the latest version, over its hash, and never changes; nothing goes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.migration_mapping_guard() RETURNS trigger
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
CREATE TRIGGER migration_mapping_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.migration_mapping
  FOR EACH ROW EXECUTE FUNCTION fyniq.migration_mapping_guard();
CREATE TRIGGER migration_signoff_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.migration_signoff
  FOR EACH ROW EXECUTE FUNCTION fyniq.migration_mapping_guard();
CREATE TRIGGER migration_mapping_no_truncate BEFORE TRUNCATE ON fyniq.migration_mapping
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER migration_signoff_no_truncate BEFORE TRUNCATE ON fyniq.migration_signoff
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.migration_mapping
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('migration_run', 'run_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.migration_signoff
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('migration_run', 'run_id');

ALTER TABLE fyniq.migration_mapping ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.migration_mapping FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.migration_mapping
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.migration_signoff ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.migration_signoff FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.migration_signoff
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.migration_mapping, fyniq.migration_signoff FROM PUBLIC;
GRANT SELECT ON fyniq.migration_mapping, fyniq.migration_signoff TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (run_id, version, step_id, mapping, mapping_sha256, made_by, made_at) ON fyniq.migration_mapping TO fyniq_api, fyniq_worker;
GRANT INSERT (run_id, subject, mapping_id, subject_sha256, signed_by, signed_at) ON fyniq.migration_signoff TO fyniq_api;
