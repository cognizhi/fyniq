-- Erasure of a former employee's personal data (PRD-002 NM-SEC-08), with dual control and a signed register outside the database.
--
-- An order is asked for by an Owner or an HR administrator once the person's retention periods have run, approved by a second one, and executed by the worker, which
-- first files a signed entry in the register (a write-once store outside this database, so a restore cannot lose it) and then calls erase_person. erase_person is the one
-- way the personal columns of the mutable records change: it refuses unless the order is approved and the day is on or after its eligible date, the person has left and
-- no legal hold keeps their records, and it empties only the columns it names. It changes no append-only row and no guard function is touched: the rows that hold the
-- person's data in plaintext in append-only tables (payslips, issued letters, pay run lines and the like) stay, tied to the person's opaque ID, and wait for the owner's decision
-- on how they are shredded (docs/security/p2-grants-for-human-review.md). After a restore, reapply_erasure does the same from a register entry the worker has verified.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.person ADD COLUMN erased_at timestamptz;

CREATE TABLE nemura.erasure_order (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  person_id       uuid        NOT NULL,
  reason          text        NOT NULL,
  eligible_on     date        NOT NULL,   -- the first day the retention periods allow it, worked out when it was asked for
  requested_by    uuid        NOT NULL,
  requested_at    timestamptz NOT NULL,
  approved_by     uuid,
  approved_at     timestamptz,
  state           text        NOT NULL DEFAULT 'requested',
  executed_at     timestamptz,
  register_sha256 bytea,                  -- of the register entry filed before the scrub
  CONSTRAINT erasure_order_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT erasure_order_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT erasure_order_requested_fkey FOREIGN KEY (organisation_id, requested_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT erasure_order_approved_fkey FOREIGN KEY (organisation_id, approved_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT erasure_order_state_known CHECK (state IN ('requested', 'approved', 'executed', 'refused')),
  CONSTRAINT erasure_order_reason_length CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
  CONSTRAINT erasure_order_second_person CHECK (approved_by IS NULL OR approved_by <> requested_by),
  CONSTRAINT erasure_order_approved_whole CHECK ((state IN ('approved', 'executed')) = (approved_by IS NOT NULL AND approved_at IS NOT NULL) OR state = 'refused'),
  CONSTRAINT erasure_order_executed_whole CHECK ((state = 'executed') = (executed_at IS NOT NULL AND register_sha256 IS NOT NULL)),
  CONSTRAINT erasure_order_sha CHECK (register_sha256 IS NULL OR octet_length(register_sha256) = 32)
);
-- one order at a time that is not finished or refused, and one execution for ever
CREATE UNIQUE INDEX erasure_order_one_live ON nemura.erasure_order (organisation_id, person_id) WHERE state IN ('requested', 'approved');
CREATE UNIQUE INDEX erasure_order_one_executed ON nemura.erasure_order (organisation_id, person_id) WHERE state = 'executed';

-- an order moves forward only: requested, then approved or refused, then executed; nothing is deleted; what was asked is not rewritten
-- +goose StatementBegin
CREATE FUNCTION nemura.erasure_order_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.reason IS DISTINCT FROM OLD.reason OR NEW.eligible_on IS DISTINCT FROM OLD.eligible_on
     OR NEW.requested_by IS DISTINCT FROM OLD.requested_by OR NEW.requested_at IS DISTINCT FROM OLD.requested_at
     OR NOT ((OLD.state = 'requested' AND NEW.state IN ('approved', 'refused')) OR (OLD.state = 'approved' AND NEW.state = 'executed')) THEN
    RAISE EXCEPTION 'An erasure order moves forward only: requested, approved or refused, then executed; it is never deleted or rewritten' USING ERRCODE = 'NM200';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER erasure_order_guard BEFORE UPDATE OR DELETE ON nemura.erasure_order FOR EACH ROW EXECUTE FUNCTION nemura.erasure_order_guard();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.erasure_order
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('erasure_order', 'id');

-- The scrub. Both functions empty exactly these columns and change nothing else; both are for the worker alone.
-- +goose StatementBegin
CREATE FUNCTION nemura.scrub_person(p_person uuid, p_on timestamptz) RETURNS void
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
  IF m IS NOT NULL THEN
    UPDATE nemura.member SET display_name = 'Former employee', deactivated_at = coalesce(deactivated_at, p_on) WHERE id = m;
  END IF;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION nemura.erase_person(p_order uuid, p_on date, p_register bytea) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  o nemura.erasure_order;
  p nemura.person;
BEGIN
  SELECT * INTO o FROM nemura.erasure_order WHERE id = p_order FOR UPDATE;
  IF NOT FOUND OR o.state <> 'approved' THEN
    RAISE EXCEPTION 'An erasure runs only from an approved order' USING ERRCODE = 'NM201';
  END IF;
  SELECT * INTO p FROM nemura.person WHERE id = o.person_id;
  IF p.leaving_date IS NULL OR p.leaving_date > p_on OR p_on < o.eligible_on THEN
    RAISE EXCEPTION 'The person has not left, or the retention periods have not run' USING ERRCODE = 'NM202';
  END IF;
  IF EXISTS (SELECT 1 FROM nemura.legal_hold h WHERE h.released_on IS NULL AND h.placed_on <= p_on
               AND (h.subject_id = o.person_id OR (h.subject_id IS NULL AND h.record_type IN ('people-records', 'pay-records')))) THEN
    RAISE EXCEPTION 'A legal hold keeps this person''s records' USING ERRCODE = 'NM203';
  END IF;
  PERFORM nemura.scrub_person(o.person_id, p_on::timestamptz);
  UPDATE nemura.erasure_order SET state = 'executed', executed_at = p_on::timestamptz, register_sha256 = p_register WHERE id = p_order;
  RETURN o.person_id;
END
$$;
-- +goose StatementEnd

-- After a restore: the register's entry, which the worker has verified, says the person was erased. The database repeats only the floor of it: the person has left and the
-- minimum employment period (MOM, one year) has passed. The longer periods were checked when the order was approved.
-- +goose StatementBegin
CREATE FUNCTION nemura.reapply_erasure(p_person uuid, p_on date) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  p nemura.person;
BEGIN
  SELECT * INTO p FROM nemura.person WHERE id = p_person;
  IF NOT FOUND THEN
    RETURN false; -- the restored database does not have the person: nothing to scrub
  END IF;
  IF p.leaving_date IS NULL OR p_on < (p.leaving_date + interval '12 months')::date THEN
    RAISE EXCEPTION 'A register entry cannot erase a person who has not left a year ago' USING ERRCODE = 'NM204';
  END IF;
  -- true only when something of the person had come back: a second run finds nothing and reports false
  IF p.erased_at IS NULL OR p.legal_name <> 'Former employee' OR p.preferred_name IS NOT NULL OR p.work_email IS NOT NULL OR p.phone IS NOT NULL OR p.leaving_reason IS NOT NULL
     OR EXISTS (SELECT 1 FROM nemura.person_contact c WHERE c.person_id = p_person AND (c.personal_email IS NOT NULL OR c.address IS NOT NULL OR c.postal_code IS NOT NULL))
     OR EXISTS (SELECT 1 FROM nemura.emergency_contact e WHERE e.person_id = p_person)
     OR EXISTS (SELECT 1 FROM nemura.person_secret x WHERE x.person_id = p_person AND (x.nric_sealed IS NOT NULL OR x.bank_sealed IS NOT NULL OR x.ir8a_sealed IS NOT NULL OR x.bank_code IS NOT NULL OR x.bank_name IS NOT NULL))
     OR EXISTS (SELECT 1 FROM nemura.member m WHERE m.id = p.member_id AND m.display_name <> 'Former employee') THEN
    PERFORM nemura.scrub_person(p_person, p_on::timestamptz);
    RETURN true;
  END IF;
  RETURN false;
END
$$;
-- +goose StatementEnd

ALTER TABLE nemura.erasure_order ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.erasure_order FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.erasure_order USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

REVOKE ALL ON nemura.erasure_order FROM PUBLIC;
REVOKE ALL ON FUNCTION nemura.scrub_person(uuid, timestamptz), nemura.erase_person(uuid, date, bytea), nemura.reapply_erasure(uuid, date) FROM PUBLIC;
GRANT SELECT ON nemura.erasure_order TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, person_id, reason, eligible_on, requested_by, requested_at) ON nemura.erasure_order TO nemura_api;
GRANT UPDATE (state, approved_by, approved_at) ON nemura.erasure_order TO nemura_api;
GRANT EXECUTE ON FUNCTION nemura.erase_person(uuid, date, bytea), nemura.reapply_erasure(uuid, date) TO nemura_worker;
