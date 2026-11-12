-- The shredding of person keys at the end of retention, and the anonymisation of contacts (PRD-001 §11.8, §11.9;
-- rev 0.244; p1-pdpa-retention slice 10, designs D9 and D10).
--
-- A shredding run lists the person keys of a financial year whose retention has ended (the year's last day plus
-- retention.records_years, at least 7). Nothing is shredded until the DPO approves the run, stepped up. The worker then
-- writes the signed fyniq-shredding@1 entry to the write-once register, records its key here, and only then calls
-- fyniq.shred_person_keys, which deletes the run's keys and marks it done. A restore re-applies the register through
-- fyniq.reapply_shredding before the copy opens. Runs never go, and change only along listed → approved → done (FY318).
--
-- An anonymisation replaces a person's contact details the records do not need (D10): the register's
-- fyniq-anonymisation@1 entry first, then fyniq.anonymise_contact. The legal name and every issued document stay.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.pd_shredding_run (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  cohort          date        NOT NULL,  -- the last day of the financial year whose keys it shreds
  keys            uuid[]      NOT NULL,  -- the person keys it shreds, as listed
  listed_at       timestamptz NOT NULL,
  status          text        NOT NULL DEFAULT 'listed',
  approved_by     text,
  approved_at     timestamptz,
  register        text,                  -- the signed register entry's key, written before the keys go
  done_at         timestamptz,
  reapplied       boolean     NOT NULL DEFAULT false,  -- recreated from the register by a restore
  version         integer     NOT NULL DEFAULT 1,
  CONSTRAINT pd_shredding_run_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pd_shredding_run_cohort_once UNIQUE (organisation_id, cohort),
  CONSTRAINT pd_shredding_run_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT pd_shredding_run_status_known CHECK (status IN ('listed', 'approved', 'done')),
  CONSTRAINT pd_shredding_run_keys_count CHECK (cardinality(keys) BETWEEN 1 AND 1000000),
  CONSTRAINT pd_shredding_run_approval CHECK ((status = 'listed') = (approved_by IS NULL) AND (approved_by IS NULL) = (approved_at IS NULL)),
  CONSTRAINT pd_shredding_run_done CHECK ((status = 'done') = (done_at IS NOT NULL) AND (status <> 'done' OR register IS NOT NULL)),
  CONSTRAINT pd_shredding_run_approved_by_format CHECK (approved_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT pd_shredding_run_register_format CHECK (register ~ '^pdpa-shredding/[0-9a-f-]{36}/[0-9a-f-]{36}\.json$'),
  CONSTRAINT pd_shredding_run_version_positive CHECK (version > 0)
);

CREATE TABLE fyniq.pd_anonymisation (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  contact_id      uuid        NOT NULL,
  request_id      uuid,                  -- the access or correction request it honours, if any
  reason          text        NOT NULL,
  register        text        NOT NULL,  -- the signed register entry's key, written first
  anonymised_by   text        NOT NULL,
  anonymised_at   timestamptz NOT NULL,
  reapplied       boolean     NOT NULL DEFAULT false,
  CONSTRAINT pd_anonymisation_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pd_anonymisation_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT pd_anonymisation_contact_fkey FOREIGN KEY (organisation_id, contact_id) REFERENCES fyniq.contact (organisation_id, id),
  CONSTRAINT pd_anonymisation_reason_length CHECK (length(btrim(reason)) BETWEEN 1 AND 1000),
  CONSTRAINT pd_anonymisation_register_format CHECK (register ~ '^pdpa-anonymisation/[0-9a-f-]{36}/[0-9a-f-]{36}\.json$'),
  CONSTRAINT pd_anonymisation_by_format CHECK (anonymised_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX pd_anonymisation_contact ON fyniq.pd_anonymisation (organisation_id, contact_id);

-- The records' retention in years for an organisation, as in effect today: at least 7 (retention.records_years).
-- +goose StatementBegin
CREATE FUNCTION fyniq.records_years(p_org uuid) RETURNS integer
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT greatest(coalesce((SELECT s.value::integer FROM fyniq.organisation_setting s
                             WHERE s.organisation_id = p_org AND s.key = 'retention.records_years' AND s.effective_from <= current_date
                             ORDER BY s.effective_from DESC, s.version DESC LIMIT 1), 7), 7)
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.records_years(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.records_years(uuid) TO fyniq_api, fyniq_worker;

-- A run moves only from listed to approved (with its approver) and on to done (inside fyniq.shred_person_keys, after
-- its register entry); what it lists never changes, and no run goes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.pd_shredding_run_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A shredding run never goes' USING ERRCODE = 'FY318';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'listed' AND coalesce(current_setting('fyniq.shredding_run', true), '') <> NEW.id::text THEN
      RAISE EXCEPTION 'A shredding run starts listed' USING ERRCODE = 'FY318';
    END IF;
    IF NEW.cohort + make_interval(years => fyniq.records_years(NEW.organisation_id)) >= current_date THEN
      RAISE EXCEPTION 'The retention of the year ending % has not ended', NEW.cohort USING ERRCODE = 'FY318',
        HINT = 'A year''s keys are shredded only after retention.records_years (§11.8).';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.cohort <> OLD.cohort OR NEW.keys <> OLD.keys OR NEW.listed_at <> OLD.listed_at OR NEW.reapplied <> OLD.reapplied THEN
    RAISE EXCEPTION 'What a shredding run lists never changes' USING ERRCODE = 'FY318';
  END IF;
  IF OLD.status = 'done'
     OR (OLD.status = 'listed' AND NEW.status NOT IN ('listed', 'approved'))
     OR (OLD.status = 'approved' AND (NEW.approved_by IS DISTINCT FROM OLD.approved_by OR NEW.approved_at IS DISTINCT FROM OLD.approved_at))
     OR (OLD.register IS NOT NULL AND NEW.register IS DISTINCT FROM OLD.register)
     OR (NEW.status = 'done' AND coalesce(current_setting('fyniq.shredding_run', true), '') <> OLD.id::text)
     OR (NEW.register IS NOT NULL AND NEW.status = 'listed') THEN
    RAISE EXCEPTION 'A shredding run is approved, then registered, then done: each once' USING ERRCODE = 'FY318';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER pd_shredding_run_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.pd_shredding_run
  FOR EACH ROW EXECUTE FUNCTION fyniq.pd_shredding_run_guard();
CREATE TRIGGER pd_shredding_run_no_truncate BEFORE TRUNCATE ON fyniq.pd_shredding_run
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.pd_shredding_run
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('pd_shredding_run', 'id');
CREATE TRIGGER pd_anonymisation_immutable BEFORE UPDATE OR DELETE ON fyniq.pd_anonymisation
  FOR EACH ROW EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER pd_anonymisation_no_truncate BEFORE TRUNCATE ON fyniq.pd_anonymisation
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.pd_anonymisation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('pd_anonymisation', 'id');

-- A person's key goes only inside its approved, registered run, once the year's retention has ended (replacing 000308's
-- guard, which asked only for the run's mark).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.person_key_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'UPDATE' THEN
    RAISE EXCEPTION 'A person''s key never changes' USING ERRCODE = 'FY317';
  END IF;
  IF TG_OP = 'DELETE' AND EXISTS (
       SELECT 1 FROM fyniq.pd_shredding_run r
        WHERE r.organisation_id = OLD.organisation_id
          AND r.id::text = coalesce(current_setting('fyniq.shredding_run', true), '')
          AND r.status = 'approved' AND r.register IS NOT NULL AND OLD.id = ANY (r.keys) AND r.cohort = OLD.cohort)
     AND OLD.cohort + make_interval(years => fyniq.records_years(OLD.organisation_id)) < current_date THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'A person''s key goes only in its approved shredding run, after the year''s retention' USING ERRCODE = 'FY317',
    HINT = 'The DPO approves the shredding run; the register entry is written first (§11.8).';
END
$$;
-- +goose StatementEnd

-- The shredding of an approved, registered run: its keys go, and it is done.
-- +goose StatementBegin
CREATE FUNCTION fyniq.shred_person_keys(p_run uuid) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
  run fyniq.pd_shredding_run;
  n   integer;
BEGIN
  SELECT * INTO run FROM fyniq.pd_shredding_run r WHERE r.organisation_id = org AND r.id = p_run FOR UPDATE;
  IF run.id IS NULL OR run.status <> 'approved' OR run.register IS NULL THEN
    RAISE EXCEPTION 'Shredding run % is not approved and registered', p_run USING ERRCODE = 'FY318',
      HINT = 'The DPO approves it, and the worker writes its register entry first (§11.8).';
  END IF;
  PERFORM set_config('fyniq.shredding_run', p_run::text, true);
  DELETE FROM fyniq.person_key k WHERE k.organisation_id = org AND k.id = ANY (run.keys);
  GET DIAGNOSTICS n = ROW_COUNT;
  UPDATE fyniq.pd_shredding_run SET status = 'done', done_at = now(), version = version + 1 WHERE organisation_id = org AND id = p_run;
  PERFORM set_config('fyniq.shredding_run', '', true);
  RETURN n;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.shred_person_keys(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.shred_person_keys(uuid) TO fyniq_worker;

-- A restore re-applies a signed register entry the worker checked (D9), before the copy opens: the run is recreated as
-- approved and registered if the copy lacks it, or brought to that state, and its keys that came back go again.
-- +goose StatementBegin
CREATE FUNCTION fyniq.reapply_shredding(p_run uuid, p_cohort date, p_keys uuid[], p_listed_at timestamptz, p_approved_by text,
  p_approved_at timestamptz, p_register text) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
  run fyniq.pd_shredding_run;
  n   integer;
BEGIN
  PERFORM set_config('fyniq.shredding_run', p_run::text, true);
  SELECT * INTO run FROM fyniq.pd_shredding_run r WHERE r.organisation_id = org AND r.id = p_run FOR UPDATE;
  IF run.id IS NULL THEN
    INSERT INTO fyniq.pd_shredding_run (organisation_id, id, cohort, keys, listed_at, status, approved_by, approved_at, register, reapplied)
    VALUES (org, p_run, p_cohort, p_keys, p_listed_at, 'approved', p_approved_by, p_approved_at, p_register, true);
  ELSIF run.status = 'done' THEN
    PERFORM set_config('fyniq.shredding_run', '', true);
    RETURN 0;  -- the copy holds the run done: its keys are gone already
  ELSE
    UPDATE fyniq.pd_shredding_run SET status = 'approved', approved_by = coalesce(approved_by, p_approved_by),
      approved_at = coalesce(approved_at, p_approved_at), register = coalesce(register, p_register), version = version + 1
    WHERE organisation_id = org AND id = p_run;
  END IF;
  DELETE FROM fyniq.person_key k WHERE k.organisation_id = org AND k.id = ANY (p_keys);
  GET DIAGNOSTICS n = ROW_COUNT;
  UPDATE fyniq.pd_shredding_run SET status = 'done', done_at = now(), version = version + 1 WHERE organisation_id = org AND id = p_run;
  PERFORM set_config('fyniq.shredding_run', '', true);
  RETURN n;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.reapply_shredding(uuid, date, uuid[], timestamptz, text, timestamptz, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.reapply_shredding(uuid, date, uuid[], timestamptz, text, timestamptz, text) TO fyniq_worker;

-- An anonymisation: a person's emails and phone go, and their address reads Anonymised; the legal name stays.
-- +goose StatementBegin
CREATE FUNCTION fyniq.anonymise_contact(p_contact uuid) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
  n   integer;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM fyniq.pd_anonymisation a WHERE a.organisation_id = org AND a.contact_id = p_contact) THEN
    RAISE EXCEPTION 'Contact % has no recorded anonymisation', p_contact USING ERRCODE = 'FY318',
      HINT = 'Record the anonymisation, with its register entry, first (§11.9).';
  END IF;
  UPDATE fyniq.contact SET billing_email = NULL, remittance_email = NULL, phone = NULL,
    address = CASE WHEN address IS NULL THEN NULL ELSE 'Anonymised' END, version = version + 1
  WHERE organisation_id = org AND id = p_contact
    AND (billing_email IS NOT NULL OR remittance_email IS NOT NULL OR phone IS NOT NULL OR address IS DISTINCT FROM 'Anonymised' AND address IS NOT NULL);
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.anonymise_contact(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.anonymise_contact(uuid) TO fyniq_api, fyniq_worker;

ALTER TABLE fyniq.pd_shredding_run ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.pd_shredding_run FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.pd_shredding_run
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.pd_anonymisation ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.pd_anonymisation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.pd_anonymisation
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (cohort, keys, listed_at) ON fyniq.pd_shredding_run TO fyniq_worker;
GRANT UPDATE (register, version) ON fyniq.pd_shredding_run TO fyniq_worker;
GRANT UPDATE (status, approved_by, approved_at, version) ON fyniq.pd_shredding_run TO fyniq_api;
GRANT SELECT ON fyniq.pd_shredding_run TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (contact_id, request_id, reason, register, anonymised_by, anonymised_at) ON fyniq.pd_anonymisation TO fyniq_api;
GRANT INSERT (contact_id, request_id, reason, register, anonymised_by, anonymised_at, reapplied) ON fyniq.pd_anonymisation TO fyniq_worker;
GRANT SELECT ON fyniq.pd_anonymisation TO fyniq_api, fyniq_worker, fyniq_readonly;
