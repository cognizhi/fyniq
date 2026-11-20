-- Retention and legal holds (PRD-001 AUD-11, rev 0.617; p2-security-certification slice 2, design D1 to D3).
--
-- The DPO or an Owner places a hold on a record (one source document), a contact or a financial year, with a reason, and lifts
-- it, with a reason: each is audited in the same transaction. A hold is never deleted and changes once, when it is lifted.
-- While one stands, the purge after retention (AUD-09, AUD-10) skips what it covers and the evidence store keeps the files.
--
-- Coverage is defined here once, in three functions the purge jobs and the triggers both call:
--   hold_covers_event   an audit event: its object is the held document or contact, or a document naming the held contact, or it
--                       occurred in a held year (read on the UTC date widened by one day each side, so no time zone leaves an
--                       event out);
--   hold_ledger_dates   the posting dates of the covered ledger entries (a held year gives its first day);
--   hold_evidence       the covered final evidence files;
--   hold_covers_person_key  a person's key for a financial year: a held contact's keys and every key of a held year, which the
--                       shredding of personal data (§11.8) neither lists nor deletes while the hold stands.
-- Each refuses to answer for an organisation other than the transaction's (FY361) and is SECURITY DEFINER, so a caller that cannot
-- read the holds fails closed to "held", never to "free". Triggers then refuse to delete a covered audit event or ledger entry,
-- and a ledger boundary after a covered date, whoever asks and even if the job is wrong. The purge functions of 000291 and
-- 000292 are not re-declared: the new guards are separate triggers.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.retention_hold (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  kind            text        NOT NULL,  -- record, contact or financial_year
  document_id     uuid,                  -- the record: a source document
  contact_id      uuid,                  -- the contact
  year_number     integer,               -- the financial year's number in the organisation's calendar
  year_starts_on  date,
  year_ends_on    date,
  reason          text        NOT NULL,
  placed_by       text        NOT NULL,
  placed_at       timestamptz NOT NULL,
  lifted_by       text,
  lifted_at       timestamptz,
  lift_reason     text,
  CONSTRAINT retention_hold_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT retention_hold_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT retention_hold_document_fkey FOREIGN KEY (organisation_id, document_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT retention_hold_contact_fkey FOREIGN KEY (organisation_id, contact_id) REFERENCES fyniq.contact (organisation_id, id),
  CONSTRAINT retention_hold_kind_known CHECK (kind IN ('record', 'contact', 'financial_year')),
  CONSTRAINT retention_hold_subject CHECK (
    (kind = 'record' AND document_id IS NOT NULL AND contact_id IS NULL AND year_starts_on IS NULL AND year_ends_on IS NULL AND year_number IS NULL)
    OR (kind = 'contact' AND contact_id IS NOT NULL AND document_id IS NULL AND year_starts_on IS NULL AND year_ends_on IS NULL AND year_number IS NULL)
    OR (kind = 'financial_year' AND document_id IS NULL AND contact_id IS NULL AND year_starts_on IS NOT NULL AND year_ends_on IS NOT NULL
        AND year_number > 0 AND year_ends_on > year_starts_on AND year_ends_on - year_starts_on < 800)),
  CONSTRAINT retention_hold_reason CHECK (length(btrim(reason)) BETWEEN 3 AND 500),
  CONSTRAINT retention_hold_placed_by_format CHECK (placed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT retention_hold_lift CHECK (
    (lifted_at IS NULL AND lifted_by IS NULL AND lift_reason IS NULL)
    OR (lifted_at IS NOT NULL AND lifted_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND length(btrim(lift_reason)) BETWEEN 3 AND 500
        AND lifted_at >= placed_at))
);
-- One hold in force on a subject: a second is refused, and a lifted one can be placed again.
CREATE UNIQUE INDEX retention_hold_record_once ON fyniq.retention_hold (organisation_id, document_id) WHERE lifted_at IS NULL AND kind = 'record';
CREATE UNIQUE INDEX retention_hold_contact_once ON fyniq.retention_hold (organisation_id, contact_id) WHERE lifted_at IS NULL AND kind = 'contact';
CREATE UNIQUE INDEX retention_hold_year_once ON fyniq.retention_hold (organisation_id, year_starts_on) WHERE lifted_at IS NULL AND kind = 'financial_year';
CREATE INDEX retention_hold_in_force ON fyniq.retention_hold (organisation_id) WHERE lifted_at IS NULL;

-- A hold is placed unlifted and changes once, when it is lifted: nothing else of it ever changes, and it is never deleted.
-- +goose StatementBegin
CREATE FUNCTION fyniq.retention_hold_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.lifted_at IS NOT NULL THEN
      RAISE EXCEPTION 'A hold is placed in force' USING ERRCODE = 'FY360', HINT = 'Place the hold, then lift it with its reason (AUD-11).';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'UPDATE' AND OLD.lifted_at IS NULL AND NEW.lifted_at IS NOT NULL
     AND (to_jsonb(NEW) - 'lifted_by' - 'lifted_at' - 'lift_reason') = (to_jsonb(OLD) - 'lifted_by' - 'lifted_at' - 'lift_reason') THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'A hold never changes or goes, but for being lifted once' USING ERRCODE = 'FY360',
    HINT = 'Lift it with a reason; place a new hold to hold again (AUD-11).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER retention_hold_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.retention_hold
  FOR EACH ROW EXECUTE FUNCTION fyniq.retention_hold_guard();
CREATE TRIGGER retention_hold_no_truncate BEFORE TRUNCATE ON fyniq.retention_hold
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.retention_hold
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('retention_hold', 'id');

ALTER TABLE fyniq.retention_hold ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.retention_hold FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.retention_hold
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- The API places a hold and lifts it, by the lift's columns only (Owner or DPO: the handler's rule). The worker and the
-- read-only role read.
GRANT INSERT (kind, document_id, contact_id, year_number, year_starts_on, year_ends_on, reason, placed_by, placed_at)
  ON fyniq.retention_hold TO fyniq_api;
GRANT UPDATE (lifted_by, lifted_at, lift_reason) ON fyniq.retention_hold TO fyniq_api;
GRANT SELECT ON fyniq.retention_hold TO fyniq_api, fyniq_worker, fyniq_readonly;

-- ------------------------------------------------------------------ coverage
-- The organisation a coverage question is asked for is the transaction's, or the answer is an error (a caller cannot ask
-- about another organisation's holds and read "none" through row-level security).
-- +goose StatementBegin
CREATE FUNCTION fyniq.hold_scope(p_org uuid) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF p_org IS DISTINCT FROM fyniq.require_org() THEN
    RAISE EXCEPTION 'Retention holds are asked for the transaction''s organisation only' USING ERRCODE = 'FY361';
  END IF;
  RETURN EXISTS (SELECT 1 FROM fyniq.retention_hold h WHERE h.organisation_id = p_org AND h.lifted_at IS NULL);
END
$$;
-- +goose StatementEnd

-- A document names a contact: as the customer, supplier or contact of any of its versions, or on a line of an entry posted from it.
-- +goose StatementBegin
CREATE FUNCTION fyniq.document_names_contact(p_org uuid, p_document uuid, p_contact uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM fyniq.document_version v
                  WHERE v.organisation_id = p_org AND v.document_id = p_document
                    AND p_contact::text IN (v.payload ->> 'customer_id', v.payload ->> 'supplier_id', v.payload ->> 'contact_id'))
      OR EXISTS (SELECT 1 FROM fyniq.journal_entry e JOIN fyniq.journal_line l ON l.organisation_id = e.organisation_id AND l.entry_id = e.id
                  WHERE e.organisation_id = p_org AND e.source_id = p_document::text AND l.party_id = p_contact)
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION fyniq.hold_covers_event(p_org uuid, p_object_id uuid, p_occurred timestamptz) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT fyniq.hold_scope(p_org) THEN
    RETURN false;
  END IF;
  RETURN EXISTS (
    SELECT 1 FROM fyniq.retention_hold h
     WHERE h.organisation_id = p_org AND h.lifted_at IS NULL AND (
       (h.kind = 'record' AND p_object_id = h.document_id)
       OR (h.kind = 'contact' AND (p_object_id = h.contact_id OR fyniq.document_names_contact(p_org, p_object_id, h.contact_id)))
       OR (h.kind = 'financial_year' AND (p_occurred AT TIME ZONE 'UTC')::date BETWEEN h.year_starts_on - 1 AND h.year_ends_on + 1)));
END
$$;
-- +goose StatementEnd

-- One entry: posted from a held document, naming a held contact (or a document that does), or dated in a held year.
-- +goose StatementBegin
CREATE FUNCTION fyniq.hold_covers_entry(p_org uuid, p_entry uuid, p_posting_date date, p_source_id text) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT fyniq.hold_scope(p_org) THEN
    RETURN false;
  END IF;
  RETURN EXISTS (
    SELECT 1 FROM fyniq.retention_hold h
     WHERE h.organisation_id = p_org AND h.lifted_at IS NULL AND (
       (h.kind = 'record' AND p_source_id = h.document_id::text)
       OR (h.kind = 'contact' AND (
             EXISTS (SELECT 1 FROM fyniq.journal_line l WHERE l.organisation_id = p_org AND l.entry_id = p_entry AND l.party_id = h.contact_id)
             OR CASE WHEN p_source_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                     THEN fyniq.document_names_contact(p_org, p_source_id::uuid, h.contact_id) ELSE false END))
       OR (h.kind = 'financial_year' AND p_posting_date BETWEEN h.year_starts_on AND h.year_ends_on)));
END
$$;
-- +goose StatementEnd

-- The posting dates of the covered entries, for the plan of the ledger's purge.
-- +goose StatementBegin
CREATE FUNCTION fyniq.hold_ledger_dates(p_org uuid) RETURNS SETOF date
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT fyniq.hold_scope(p_org) THEN
    RETURN;
  END IF;
  RETURN QUERY
    SELECT h.year_starts_on FROM fyniq.retention_hold h
     WHERE h.organisation_id = p_org AND h.lifted_at IS NULL AND h.kind = 'financial_year'
    UNION
    SELECT e.posting_date FROM fyniq.retention_hold h JOIN fyniq.journal_entry e ON e.organisation_id = h.organisation_id
     WHERE h.organisation_id = p_org AND h.lifted_at IS NULL AND h.kind = 'record' AND e.source_id = h.document_id::text
    UNION
    SELECT e.posting_date FROM fyniq.retention_hold h JOIN fyniq.journal_entry e ON e.organisation_id = h.organisation_id
     WHERE h.organisation_id = p_org AND h.lifted_at IS NULL AND h.kind = 'contact'
       AND fyniq.hold_covers_entry(p_org, e.id, e.posting_date, e.source_id);
END
$$;
-- +goose StatementEnd

-- The final evidence files a hold in force covers: those of a held or contact-named document, and those relating to, or on a
-- document dated in, a held year.
-- +goose StatementBegin
CREATE FUNCTION fyniq.hold_evidence(p_org uuid) RETURNS SETOF uuid
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT fyniq.hold_scope(p_org) THEN
    RETURN;
  END IF;
  RETURN QUERY
    SELECT ev.id FROM fyniq.evidence ev
     WHERE ev.organisation_id = p_org AND ev.state = 'final' AND ev.locked_key IS NOT NULL AND EXISTS (
       SELECT 1 FROM fyniq.retention_hold h
        WHERE h.organisation_id = p_org AND h.lifted_at IS NULL AND (
          (h.kind = 'record' AND ev.document_id = h.document_id)
          OR (h.kind = 'contact' AND fyniq.document_names_contact(p_org, ev.document_id, h.contact_id))
          OR (h.kind = 'financial_year' AND (
                ev.relates_on BETWEEN h.year_starts_on AND h.year_ends_on
                OR EXISTS (SELECT 1 FROM fyniq.document_version v
                            WHERE v.organisation_id = p_org AND v.document_id = ev.document_id AND v.doc_date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'
                              AND v.doc_date::date BETWEEN h.year_starts_on AND h.year_ends_on)))));
END
$$;
-- +goose StatementEnd

-- A person's key for a financial year (its last day): the contact's own, or any of a held year.
-- +goose StatementBegin
CREATE FUNCTION fyniq.hold_covers_person_key(p_org uuid, p_person text, p_cohort date) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT fyniq.hold_scope(p_org) THEN
    RETURN false;
  END IF;
  RETURN EXISTS (
    SELECT 1 FROM fyniq.retention_hold h
     WHERE h.organisation_id = p_org AND h.lifted_at IS NULL AND (
       (h.kind = 'contact' AND p_person = 'contact:' || h.contact_id::text)
       OR (h.kind = 'financial_year' AND p_cohort BETWEEN h.year_starts_on AND h.year_ends_on)));
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.hold_scope(uuid), fyniq.document_names_contact(uuid, uuid, uuid), fyniq.hold_covers_event(uuid, uuid, timestamptz),
  fyniq.hold_covers_entry(uuid, uuid, date, text), fyniq.hold_ledger_dates(uuid), fyniq.hold_evidence(uuid),
  fyniq.hold_covers_person_key(uuid, text, date) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.hold_covers_event(uuid, uuid, timestamptz), fyniq.hold_ledger_dates(uuid), fyniq.hold_evidence(uuid),
  fyniq.hold_covers_entry(uuid, uuid, date, text), fyniq.hold_covers_person_key(uuid, text, date) TO fyniq_api, fyniq_worker;

-- ------------------------------------------------------------------ the backstops
-- A covered audit event is not deleted, even by the purge function of 000291.
-- +goose StatementBegin
CREATE FUNCTION fyniq.audit_event_hold_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF fyniq.hold_covers_event(OLD.organisation_id, OLD.object_id, (OLD.body::jsonb ->> 'occurred_at')::timestamptz) THEN
    RAISE EXCEPTION 'A legal hold covers this audit event' USING ERRCODE = 'FY361',
      HINT = 'Lift the hold with a reason before the purge may remove it (AUD-11).';
  END IF;
  RETURN OLD;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER audit_event_hold_guard BEFORE DELETE ON fyniq.audit_event
  FOR EACH ROW EXECUTE FUNCTION fyniq.audit_event_hold_guard();

-- A covered ledger entry is not deleted, even by the purge function of 000292.
-- +goose StatementBegin
CREATE FUNCTION fyniq.journal_entry_hold_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF fyniq.hold_covers_entry(OLD.organisation_id, OLD.id, OLD.posting_date, OLD.source_id) THEN
    RAISE EXCEPTION 'A legal hold covers this ledger entry' USING ERRCODE = 'FY361',
      HINT = 'Lift the hold with a reason before the purge may remove it (AUD-11).';
  END IF;
  RETURN OLD;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER journal_entry_hold_guard BEFORE DELETE ON fyniq.journal_entry
  FOR EACH ROW EXECUTE FUNCTION fyniq.journal_entry_hold_guard();

-- A boundary is not recorded after a covered date of the year it would purge (a date before the previous boundary is of a
-- year already gone, whose kept entries stay).
-- +goose StatementBegin
CREATE FUNCTION fyniq.ledger_boundary_hold_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  previous date;
BEGIN
  SELECT max(b.boundary) INTO previous FROM fyniq.ledger_boundary b WHERE b.organisation_id = NEW.organisation_id;
  IF EXISTS (SELECT 1 FROM fyniq.hold_ledger_dates(NEW.organisation_id) d
              WHERE d < NEW.boundary AND d >= coalesce(previous, '-infinity'::date)) THEN
    RAISE EXCEPTION 'A legal hold covers the year before %', NEW.boundary USING ERRCODE = 'FY361',
      HINT = 'Lift the hold with a reason before that year may be purged (AUD-11).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER ledger_boundary_hold_guard BEFORE INSERT ON fyniq.ledger_boundary
  FOR EACH ROW EXECUTE FUNCTION fyniq.ledger_boundary_hold_guard();

-- A held person's key is not shredded, even in an approved run: the run fails whole (the DPO sees it) until the hold is lifted.
-- +goose StatementBegin
CREATE FUNCTION fyniq.person_key_hold_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF fyniq.hold_covers_person_key(OLD.organisation_id, OLD.person, OLD.cohort) THEN
    RAISE EXCEPTION 'A legal hold covers this person''s key' USING ERRCODE = 'FY361',
      HINT = 'Lift the hold with a reason before the key may be shredded (AUD-11, §11.8).';
  END IF;
  RETURN OLD;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER person_key_hold_guard BEFORE DELETE ON fyniq.person_key
  FOR EACH ROW EXECUTE FUNCTION fyniq.person_key_hold_guard();
