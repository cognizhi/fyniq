-- Leave requests (PRD-002 NM-LV-03, NM-LV-04; p1-nemura-leave-time slice 2).
--
-- A request is a person's ask for leave on dates: its chargeable days are worked out when it is made and reserved in the ledger in the same
-- transaction (design D2). Approving it keeps the reservation; refusing or cancelling releases it; once the leave is taken the reservation becomes a use.
-- A request is evidence: apart from its decision and the day it was settled it never changes, and the database refuses an approver who is the requester.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.leave_request (
  organisation_id uuid         NOT NULL DEFAULT nemura.current_org(),
  id              uuid         NOT NULL DEFAULT uuidv7(),
  person_id       uuid         NOT NULL,
  type_key        text         NOT NULL,
  leave_year      integer      NOT NULL,
  start_date      date         NOT NULL,
  end_date        date         NOT NULL,
  start_half      boolean      NOT NULL DEFAULT false,
  end_half        boolean      NOT NULL DEFAULT false,
  days            numeric(8,2) NOT NULL,
  note            text,
  state           text         NOT NULL DEFAULT 'pending',
  requested_by    uuid         NOT NULL,
  requested_at    timestamptz  NOT NULL DEFAULT now(),
  decided_by      uuid,
  decided_at      timestamptz,
  decision_reason text,
  settled_at      timestamptz,
  CONSTRAINT leave_request_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT leave_request_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT leave_request_requested_by_fkey FOREIGN KEY (organisation_id, requested_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT leave_request_decided_by_fkey FOREIGN KEY (organisation_id, decided_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT leave_request_type_key CHECK (type_key ~ '^[a-z][a-z0-9_.]{1,60}$'),
  CONSTRAINT leave_request_dates CHECK (end_date >= start_date AND extract(year FROM start_date) = leave_year AND extract(year FROM end_date) = leave_year),
  CONSTRAINT leave_request_days CHECK (days > 0),
  CONSTRAINT leave_request_state_known CHECK (state IN ('pending', 'approved', 'refused', 'cancelled')),
  CONSTRAINT leave_request_decided_whole CHECK ((state = 'pending') = (decided_by IS NULL) AND (state = 'pending') = (decided_at IS NULL)),
  CONSTRAINT leave_request_refusal_reason CHECK (state <> 'refused' OR length(btrim(coalesce(decision_reason, ''))) > 0),
  CONSTRAINT leave_request_no_self_decision CHECK (state NOT IN ('approved', 'refused') OR decided_by <> requested_by),
  CONSTRAINT leave_request_settled_approved CHECK (settled_at IS NULL OR state = 'approved'),
  CONSTRAINT leave_request_note_length CHECK (note IS NULL OR length(note) <= 500),
  CONSTRAINT leave_request_reason_length CHECK (decision_reason IS NULL OR length(decision_reason) <= 500)
);
CREATE INDEX leave_request_person_idx ON nemura.leave_request (organisation_id, person_id, start_date);
CREATE INDEX leave_request_state_idx ON nemura.leave_request (organisation_id, state, start_date);

-- A request moves one way: pending to approved, refused or cancelled; approved to cancelled; its dates, days and person never change; a decision is
-- written once; and a person's pending and approved requests never overlap (the person is locked for the check, so two at once cannot both pass).
-- +goose StatementBegin
CREATE FUNCTION nemura.leave_request_guard() RETURNS trigger
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
                 AND daterange(r.start_date, r.end_date, '[]') && daterange(NEW.start_date, NEW.end_date, '[]')) THEN
      RAISE EXCEPTION 'The person has leave requested or approved on some of those days' USING ERRCODE = 'NM145';
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
CREATE TRIGGER leave_request_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.leave_request FOR EACH ROW EXECUTE FUNCTION nemura.leave_request_guard();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.leave_request DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('leave_request', 'id');

ALTER TABLE nemura.leave_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.leave_request FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.leave_request USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.leave_request TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, person_id, type_key, leave_year, start_date, end_date, start_half, end_half, days, note, requested_by) ON nemura.leave_request TO nemura_api;
GRANT UPDATE (state, decided_by, decided_at, decision_reason, settled_at) ON nemura.leave_request TO nemura_api;

-- A ledger entry for a request names a request that exists (not validated here: the ledger holds entries from before this migration, none of which name one).
ALTER TABLE nemura.leave_ledger ADD CONSTRAINT leave_ledger_request_fkey FOREIGN KEY (organisation_id, request_id) REFERENCES nemura.leave_request (organisation_id, id) NOT VALID;
