-- Government-paid leave and the employer's reimbursement claims (PRD-002 NM-LV-07, NM-LV-14; p1-nemura-leave-time slice 3).
--
-- A block of maternity, paternity or shared parental leave is a record of days the person is away on a scheme, with the scheme's rule as the country pack read on the day
-- it was recorded. The employer's claim for the Government's share is filed on the Government-Paid Leave portal by a person; Nemura keeps the claim, its deadline and
-- its receipts and rejections, and the salary owed never depends on it. Nothing here moves money. A block never overlaps other leave of the person.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.gov_leave (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  person_id       uuid        NOT NULL,
  scheme          text        NOT NULL,
  scheme_key      text        NOT NULL,   -- the pack row it falls under: maternity_citizen_1st_2nd, paternity_citizen, shared_parental_10_weeks ...
  pack_version    text        NOT NULL,
  dependant_id    uuid        NOT NULL,
  child_order     smallint    NOT NULL,
  start_date      date        NOT NULL,
  end_date        date        NOT NULL,
  days            integer     NOT NULL,   -- calendar days: a continuous block counts every day (NM-LV-11)
  claim_deadline  date        NOT NULL,
  recorded_by     uuid        NOT NULL,
  recorded_at     timestamptz NOT NULL,
  CONSTRAINT gov_leave_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT gov_leave_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT gov_leave_dependant_fkey FOREIGN KEY (organisation_id, dependant_id) REFERENCES nemura.person_dependant (organisation_id, id),
  CONSTRAINT gov_leave_by_fkey FOREIGN KEY (organisation_id, recorded_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT gov_leave_scheme_known CHECK (scheme IN ('maternity', 'paternity', 'shared_parental')),
  CONSTRAINT gov_leave_dates CHECK (end_date >= start_date AND days = (end_date - start_date) + 1),
  CONSTRAINT gov_leave_child_order CHECK (child_order BETWEEN 1 AND 20),
  CONSTRAINT gov_leave_deadline CHECK (claim_deadline > end_date)
);
CREATE INDEX gov_leave_person_idx ON nemura.gov_leave (organisation_id, person_id, start_date);

-- One test for "the person already has leave on these days": a leave request pending or approved, a sick leave notification or a government-paid block.
-- +goose StatementBegin
CREATE FUNCTION nemura.person_has_leave(p_org uuid, p_person uuid, p_from date, p_to date) RETURNS boolean
LANGUAGE sql STABLE
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM nemura.leave_request r WHERE r.organisation_id = p_org AND r.person_id = p_person AND r.state IN ('pending', 'approved')
                   AND daterange(r.start_date, r.end_date, '[]') && daterange(p_from, p_to, '[]'))
      OR EXISTS (SELECT 1 FROM nemura.sick_notification s WHERE s.organisation_id = p_org AND s.person_id = p_person
                   AND daterange(s.start_date, s.end_date, '[]') && daterange(p_from, p_to, '[]'))
      OR EXISTS (SELECT 1 FROM nemura.gov_leave g WHERE g.organisation_id = p_org AND g.person_id = p_person
                   AND daterange(g.start_date, g.end_date, '[]') && daterange(p_from, p_to, '[]'))
$$;
-- +goose StatementEnd

CREATE TABLE nemura.gov_claim (
  organisation_id uuid          NOT NULL DEFAULT nemura.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  gov_leave_id    uuid          NOT NULL,
  amount          numeric(20,4) NOT NULL,   -- SGD claimed, to the cent
  filed_on        date          NOT NULL,
  reference       text,                     -- the portal's reference for the claim
  over_guide      boolean       NOT NULL,   -- the amount is above what the scheme's cap suggests: recorded, flagged, never refused
  filed_by        uuid          NOT NULL,
  created_at      timestamptz   NOT NULL,
  CONSTRAINT gov_claim_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT gov_claim_leave_fkey FOREIGN KEY (organisation_id, gov_leave_id) REFERENCES nemura.gov_leave (organisation_id, id),
  CONSTRAINT gov_claim_by_fkey FOREIGN KEY (organisation_id, filed_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT gov_claim_amount CHECK (amount > 0 AND amount = round(amount, 2)),
  CONSTRAINT gov_claim_reference_length CHECK (reference IS NULL OR length(btrim(reference)) BETWEEN 1 AND 100)
);
CREATE INDEX gov_claim_leave_idx ON nemura.gov_claim (organisation_id, gov_leave_id);

CREATE TABLE nemura.gov_claim_event (
  organisation_id uuid          NOT NULL DEFAULT nemura.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  claim_id        uuid          NOT NULL,
  kind            text          NOT NULL,
  amount          numeric(20,4) NOT NULL DEFAULT 0,
  happened_on     date          NOT NULL,
  note            text,
  recorded_by     uuid          NOT NULL,
  created_at      timestamptz   NOT NULL,
  CONSTRAINT gov_claim_event_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT gov_claim_event_claim_fkey FOREIGN KEY (organisation_id, claim_id) REFERENCES nemura.gov_claim (organisation_id, id),
  CONSTRAINT gov_claim_event_by_fkey FOREIGN KEY (organisation_id, recorded_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT gov_claim_event_kind CHECK (kind IN ('receipt', 'rejection')),
  CONSTRAINT gov_claim_event_amount CHECK ((kind = 'receipt' AND amount > 0 AND amount = round(amount, 2)) OR (kind = 'rejection' AND amount = 0)),
  CONSTRAINT gov_claim_event_note_length CHECK (note IS NULL OR length(note) <= 500)
);
CREATE INDEX gov_claim_event_claim_idx ON nemura.gov_claim_event (organisation_id, claim_id, happened_on);

-- Government-paid leave, claims and their events are evidence: written once, never edited or deleted; a block never overlaps other leave of the person.
-- +goose StatementBegin
CREATE FUNCTION nemura.gov_leave_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION '% is evidence: a correction is a new record', TG_ARGV[0] USING ERRCODE = 'NM149';
  END IF;
  IF TG_TABLE_NAME = 'gov_leave' THEN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.organisation_id::text || NEW.person_id::text, 0));
    IF nemura.person_has_leave(NEW.organisation_id, NEW.person_id, NEW.start_date, NEW.end_date) THEN
      RAISE EXCEPTION 'The person has leave, sick leave or government-paid leave on some of those days' USING ERRCODE = 'NM150';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER gov_leave_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.gov_leave FOR EACH ROW EXECUTE FUNCTION nemura.gov_leave_guard('A government-paid leave record');
CREATE TRIGGER gov_claim_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.gov_claim FOR EACH ROW EXECUTE FUNCTION nemura.gov_leave_guard('A claim');
CREATE TRIGGER gov_claim_event_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.gov_claim_event FOR EACH ROW EXECUTE FUNCTION nemura.gov_leave_guard('A claim event');

-- Both older guards now ask the one question.
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
    IF nemura.person_has_leave(NEW.organisation_id, NEW.person_id, NEW.start_date, NEW.end_date) THEN
      RAISE EXCEPTION 'The person has leave requested, approved, sick or government-paid on some of those days' USING ERRCODE = 'NM145';
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
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.sick_notification_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A sick leave notification is evidence: it is never deleted' USING ERRCODE = 'NM146';
  END IF;
  IF TG_OP = 'INSERT' THEN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.organisation_id::text || NEW.person_id::text, 0));
    IF nemura.person_has_leave(NEW.organisation_id, NEW.person_id, NEW.start_date, NEW.end_date) THEN
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

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.gov_leave DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('gov_leave', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.gov_claim DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('gov_claim', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.gov_claim_event DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('gov_claim_event', 'id');

ALTER TABLE nemura.gov_leave ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.gov_leave FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.gov_leave USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.gov_claim ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.gov_claim FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.gov_claim USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.gov_claim_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.gov_claim_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.gov_claim_event USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.gov_leave, nemura.gov_claim, nemura.gov_claim_event TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, person_id, scheme, scheme_key, pack_version, dependant_id, child_order, start_date, end_date, days, claim_deadline, recorded_by, recorded_at) ON nemura.gov_leave TO nemura_api;
GRANT INSERT (id, gov_leave_id, amount, filed_on, reference, over_guide, filed_by, created_at) ON nemura.gov_claim TO nemura_api;
GRANT INSERT (id, claim_id, kind, amount, happened_on, note, recorded_by, created_at) ON nemura.gov_claim_event TO nemura_api;
-- The guards run as the caller, so the caller may ask the question.
GRANT EXECUTE ON FUNCTION nemura.person_has_leave(uuid, uuid, date, date) TO nemura_api;
