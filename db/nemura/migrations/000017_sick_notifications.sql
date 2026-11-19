-- Sick leave notifications (PRD-002 NM-LV-06, NM-LV-12; p1-nemura-leave-time slice 2).
--
-- Sick leave is notified, not applied for: the person tells the company on or after the first day, the certificate follows on return, and HR verifies it. The
-- days that fall within the year's paid ceiling are taken from the ledger at once; the rest are recorded as unpaid sick days for the people team to decide on.
-- The certificate is a restricted document of the person (NM-PPL-05); this table only points at it. A notification is evidence: it takes a certificate once and a
-- verification once, by someone other than the person.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.sick_notification (
  organisation_id      uuid         NOT NULL DEFAULT nemura.current_org(),
  id                   uuid         NOT NULL DEFAULT uuidv7(),
  person_id            uuid         NOT NULL,
  kind                 text         NOT NULL,
  leave_year           integer      NOT NULL,
  start_date           date         NOT NULL,
  end_date             date         NOT NULL,
  days                 numeric(8,2) NOT NULL,
  paid_days            numeric(8,2) NOT NULL,
  unpaid_days          numeric(8,2) NOT NULL,
  late                 boolean      NOT NULL,
  note                 text,
  notified_by          uuid         NOT NULL,
  notified_at          timestamptz  NOT NULL,
  certificate_id       uuid,
  issuer               text,
  certificate_added_at timestamptz,
  verified_by          uuid,
  verified_at          timestamptz,
  CONSTRAINT sick_notification_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT sick_notification_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT sick_notification_by_fkey FOREIGN KEY (organisation_id, notified_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT sick_notification_certificate_fkey FOREIGN KEY (organisation_id, certificate_id) REFERENCES nemura.person_document (organisation_id, id),
  CONSTRAINT sick_notification_verifier_fkey FOREIGN KEY (organisation_id, verified_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT sick_notification_kind_known CHECK (kind IN ('outpatient', 'hospitalisation')),
  CONSTRAINT sick_notification_dates CHECK (end_date >= start_date AND extract(year FROM start_date) = leave_year AND extract(year FROM end_date) = leave_year),
  CONSTRAINT sick_notification_days CHECK (days > 0 AND paid_days >= 0 AND unpaid_days >= 0 AND paid_days + unpaid_days = days),
  CONSTRAINT sick_notification_note_length CHECK (note IS NULL OR length(note) <= 500),
  CONSTRAINT sick_notification_issuer_length CHECK (issuer IS NULL OR length(btrim(issuer)) BETWEEN 1 AND 200),
  CONSTRAINT sick_notification_certificate_whole CHECK ((certificate_id IS NULL) = (issuer IS NULL) AND (certificate_id IS NULL) = (certificate_added_at IS NULL)),
  CONSTRAINT sick_notification_verified_whole CHECK ((verified_by IS NULL) = (verified_at IS NULL) AND (verified_by IS NULL OR certificate_id IS NOT NULL))
);
CREATE INDEX sick_notification_person_idx ON nemura.sick_notification (organisation_id, person_id, start_date);

-- +goose StatementBegin
CREATE FUNCTION nemura.sick_notification_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A sick leave notification is evidence: it is never deleted' USING ERRCODE = 'NM146';
  END IF;
  IF TG_OP = 'INSERT' THEN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.organisation_id::text || NEW.person_id::text, 0));
    IF EXISTS (SELECT 1 FROM nemura.sick_notification s WHERE s.organisation_id = NEW.organisation_id AND s.person_id = NEW.person_id
                 AND daterange(s.start_date, s.end_date, '[]') && daterange(NEW.start_date, NEW.end_date, '[]'))
       OR EXISTS (SELECT 1 FROM nemura.leave_request r WHERE r.organisation_id = NEW.organisation_id AND r.person_id = NEW.person_id AND r.state IN ('pending', 'approved')
                 AND daterange(r.start_date, r.end_date, '[]') && daterange(NEW.start_date, NEW.end_date, '[]')) THEN
      RAISE EXCEPTION 'The person has sick leave or other leave on some of those days' USING ERRCODE = 'NM147';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.kind IS DISTINCT FROM OLD.kind OR NEW.leave_year IS DISTINCT FROM OLD.leave_year OR NEW.start_date IS DISTINCT FROM OLD.start_date
     OR NEW.end_date IS DISTINCT FROM OLD.end_date OR NEW.days IS DISTINCT FROM OLD.days OR NEW.paid_days IS DISTINCT FROM OLD.paid_days OR NEW.unpaid_days IS DISTINCT FROM OLD.unpaid_days
     OR NEW.late IS DISTINCT FROM OLD.late OR NEW.note IS DISTINCT FROM OLD.note OR NEW.notified_by IS DISTINCT FROM OLD.notified_by OR NEW.notified_at IS DISTINCT FROM OLD.notified_at THEN
    RAISE EXCEPTION 'A sick leave notification is evidence: only its certificate and its verification are added' USING ERRCODE = 'NM146';
  END IF;
  IF OLD.certificate_id IS NOT NULL AND (NEW.certificate_id IS DISTINCT FROM OLD.certificate_id OR NEW.issuer IS DISTINCT FROM OLD.issuer OR NEW.certificate_added_at IS DISTINCT FROM OLD.certificate_added_at) THEN
    RAISE EXCEPTION 'A certificate is given once' USING ERRCODE = 'NM146';
  END IF;
  IF OLD.verified_by IS NOT NULL AND (NEW.verified_by IS DISTINCT FROM OLD.verified_by OR NEW.verified_at IS DISTINCT FROM OLD.verified_at) THEN
    RAISE EXCEPTION 'A certificate is verified once' USING ERRCODE = 'NM146';
  END IF;
  IF NEW.verified_by IS NOT NULL AND OLD.verified_by IS NULL AND EXISTS (SELECT 1 FROM nemura.person p WHERE p.organisation_id = NEW.organisation_id AND p.id = NEW.person_id AND p.member_id = NEW.verified_by) THEN
    RAISE EXCEPTION 'Nobody verifies their own certificate' USING ERRCODE = 'NM148';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER sick_notification_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.sick_notification FOR EACH ROW EXECUTE FUNCTION nemura.sick_notification_guard();

-- A leave request is refused on days the person has already told the company they are sick: nothing is subtracted twice.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.leave_request_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A leave request is evidence: it is cancelled, never deleted' USING ERRCODE = 'NM144';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'pending' THEN
      RAISE EXCEPTION 'A leave request starts pending' USING ERRCODE = 'NM144';
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.organisation_id::text || NEW.person_id::text, 0));
    IF EXISTS (SELECT 1 FROM nemura.leave_request r WHERE r.organisation_id = NEW.organisation_id AND r.person_id = NEW.person_id AND r.state IN ('pending', 'approved')
                 AND daterange(r.start_date, r.end_date, '[]') && daterange(NEW.start_date, NEW.end_date, '[]'))
       OR EXISTS (SELECT 1 FROM nemura.sick_notification s WHERE s.organisation_id = NEW.organisation_id AND s.person_id = NEW.person_id
                 AND daterange(s.start_date, s.end_date, '[]') && daterange(NEW.start_date, NEW.end_date, '[]')) THEN
      RAISE EXCEPTION 'The person has leave requested, approved or sick on some of those days' USING ERRCODE = 'NM145';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.type_key IS DISTINCT FROM OLD.type_key OR NEW.leave_year IS DISTINCT FROM OLD.leave_year
     OR NEW.start_date IS DISTINCT FROM OLD.start_date OR NEW.end_date IS DISTINCT FROM OLD.end_date OR NEW.start_half IS DISTINCT FROM OLD.start_half
     OR NEW.end_half IS DISTINCT FROM OLD.end_half OR NEW.days IS DISTINCT FROM OLD.days OR NEW.note IS DISTINCT FROM OLD.note
     OR NEW.requested_by IS DISTINCT FROM OLD.requested_by OR NEW.requested_at IS DISTINCT FROM OLD.requested_at THEN
    RAISE EXCEPTION 'A leave request is evidence: only its decision changes' USING ERRCODE = 'NM144';
  END IF;
  IF NEW.state IS DISTINCT FROM OLD.state AND NOT ((OLD.state = 'pending' AND NEW.state IN ('approved', 'refused', 'cancelled')) OR (OLD.state = 'approved' AND NEW.state = 'cancelled')) THEN
    RAISE EXCEPTION 'A % leave request cannot become %', OLD.state, NEW.state USING ERRCODE = 'NM144';
  END IF;
  IF OLD.decided_by IS NOT NULL AND NEW.state IN ('approved', 'refused') AND (NEW.decided_by IS DISTINCT FROM OLD.decided_by OR NEW.decided_at IS DISTINCT FROM OLD.decided_at OR NEW.decision_reason IS DISTINCT FROM OLD.decision_reason) THEN
    RAISE EXCEPTION 'A decision is written once' USING ERRCODE = 'NM144';
  END IF;
  IF OLD.settled_at IS NOT NULL AND NEW.settled_at IS DISTINCT FROM OLD.settled_at THEN
    RAISE EXCEPTION 'A settled request stays settled' USING ERRCODE = 'NM144';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.sick_notification DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('sick_notification', 'id');

ALTER TABLE nemura.sick_notification ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.sick_notification FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.sick_notification USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.sick_notification TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, person_id, kind, leave_year, start_date, end_date, days, paid_days, unpaid_days, late, note, notified_by, notified_at) ON nemura.sick_notification TO nemura_api;
GRANT UPDATE (certificate_id, issuer, certificate_added_at, verified_by, verified_at) ON nemura.sick_notification TO nemura_api;
