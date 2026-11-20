-- A key for each person's new payroll detail, destroyed by an erasure (PRD-002 NM-SEC-08; owner's decision of 21 Nov 2026).
--
-- A payslip is append-only evidence, so its detail cannot be deleted afterwards. From this migration the itemised figures of a new payslip are kept sealed under a key of the person's own
-- (payslip.data_sealed; the plain data column holds only a marker), and the key is kept wrapped by the deployment's key service in person_key. An erasure deletes the person's key in the same
-- transaction that empties their records, so the sealed detail of every payslip of theirs can never be read again, while the append-only row stays and no guard is touched. A restore that brings
-- the key back is undone by reapply_erasure. Payslips made before this migration keep their plain data; the PDF, deleted with the person's files, is the readable copy of both.
-- The sums the pay runs, the CPF file and IR8A use come from the run's own lines, not from payslip data; sealing those lines too is not done here (they are summed in SQL).

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.person_key (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  person_id       uuid        NOT NULL,
  wrapped         bytea       NOT NULL,   -- the person's data key, sealed by the key service, bound to the organisation and the person
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT person_key_pkey PRIMARY KEY (organisation_id, person_id),
  CONSTRAINT person_key_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT person_key_wrapped_length CHECK (octet_length(wrapped) BETWEEN 16 AND 512)
);

ALTER TABLE nemura.payslip ADD COLUMN data_sealed bytea;
ALTER TABLE nemura.payslip ADD CONSTRAINT payslip_sealed_has_marker CHECK (data_sealed IS NULL OR data = '{"sealed": true}'::jsonb) NOT VALID;

-- a key is written once and never changed; none is made for a person whose data was erased
-- +goose StatementBegin
CREATE FUNCTION nemura.person_key_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'UPDATE' THEN
    RAISE EXCEPTION 'A person''s key is written once and never changed' USING ERRCODE = 'NM205';
  END IF;
  IF EXISTS (SELECT 1 FROM nemura.person p WHERE p.organisation_id = NEW.organisation_id AND p.id = NEW.person_id AND p.erased_at IS NOT NULL) THEN
    RAISE EXCEPTION 'No key is made for a person whose data was erased' USING ERRCODE = 'NM205';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER person_key_guard BEFORE INSERT OR UPDATE ON nemura.person_key FOR EACH ROW EXECUTE FUNCTION nemura.person_key_guard();

ALTER TABLE nemura.person_key ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.person_key FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.person_key USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
REVOKE ALL ON nemura.person_key FROM PUBLIC;
GRANT SELECT ON nemura.person_key TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (person_id, wrapped) ON nemura.person_key TO nemura_api;
GRANT INSERT (data_sealed) ON nemura.payslip TO nemura_api;

-- The erasure destroys the key with the rest; a restore that brought it back is undone by reapply_erasure.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.scrub_person(p_person uuid, p_on timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  m uuid;
BEGIN
  UPDATE nemura.person SET legal_name = 'Former employee', preferred_name = NULL, work_email = NULL, phone = NULL, leaving_reason = NULL, erased_at = p_on
   WHERE id = p_person RETURNING member_id INTO m;
  UPDATE nemura.person_contact SET personal_email = NULL, address = NULL, postal_code = NULL, updated_at = p_on WHERE person_id = p_person;
  DELETE FROM nemura.emergency_contact WHERE person_id = p_person;
  UPDATE nemura.person_secret SET nric_sealed = NULL, nric_masked = NULL, nric_fingerprint = NULL, bank_sealed = NULL, bank_masked = NULL, bank_fingerprint = NULL,
         bank_code = NULL, bank_name = NULL, ir8a_sealed = NULL, updated_at = p_on WHERE person_id = p_person;
  DELETE FROM nemura.person_key WHERE person_id = p_person;
  IF m IS NOT NULL THEN
    UPDATE nemura.member SET display_name = 'Former employee', deactivated_at = coalesce(deactivated_at, p_on) WHERE id = m;
  END IF;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.reapply_erasure(p_person uuid, p_on date) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  p nemura.person;
BEGIN
  SELECT * INTO p FROM nemura.person WHERE id = p_person;
  IF NOT FOUND THEN
    RETURN false;
  END IF;
  IF p.leaving_date IS NULL OR p_on < (p.leaving_date + interval '12 months')::date THEN
    RAISE EXCEPTION 'A register entry cannot erase a person who has not left a year ago' USING ERRCODE = 'NM204';
  END IF;
  IF p.erased_at IS NULL OR p.legal_name <> 'Former employee' OR p.preferred_name IS NOT NULL OR p.work_email IS NOT NULL OR p.phone IS NOT NULL OR p.leaving_reason IS NOT NULL
     OR EXISTS (SELECT 1 FROM nemura.person_contact c WHERE c.person_id = p_person AND (c.personal_email IS NOT NULL OR c.address IS NOT NULL OR c.postal_code IS NOT NULL))
     OR EXISTS (SELECT 1 FROM nemura.emergency_contact e WHERE e.person_id = p_person)
     OR EXISTS (SELECT 1 FROM nemura.person_secret x WHERE x.person_id = p_person AND (x.nric_sealed IS NOT NULL OR x.bank_sealed IS NOT NULL OR x.ir8a_sealed IS NOT NULL OR x.bank_code IS NOT NULL OR x.bank_name IS NOT NULL))
     OR EXISTS (SELECT 1 FROM nemura.person_key k WHERE k.person_id = p_person)
     OR EXISTS (SELECT 1 FROM nemura.member m WHERE m.id = p.member_id AND m.display_name <> 'Former employee') THEN
    PERFORM nemura.scrub_person(p_person, p_on::timestamptz);
    RETURN true;
  END IF;
  RETURN false;
END
$$;
-- +goose StatementEnd
