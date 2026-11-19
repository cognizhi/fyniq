-- Hours worked on special days and what leave gives to a pay run (PRD-002 NM-TIM-02, NM-TIM-04, NM-LV-08; p1-nemura-leave-time slice 4).
--
-- Overtime, rest-day work and public-holiday work are recorded per day for a Part IV employee, without clocking, approved by someone other than the person, and
-- used once by a pay run. Work beyond the monthly limit stays recorded and is flagged for HR, never deleted. Unpaid leave and the encashment of annual leave become
-- inputs of the next pay run, each linked to its source and used once. Nothing here pays anyone: the pay run (a later change) reads these and marks them used.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.overtime_entry (
  organisation_id uuid         NOT NULL DEFAULT nemura.current_org(),
  id              uuid         NOT NULL DEFAULT uuidv7(),
  person_id       uuid         NOT NULL,
  work_date       date         NOT NULL,
  kind            text         NOT NULL,
  hours           numeric(4,2) NOT NULL,
  branch          text,                    -- for rest-day work: at whose request, which decides the pay
  over_limit      boolean      NOT NULL,   -- the month's overtime hours pass the pack's limit with this entry
  note            text,
  state           text         NOT NULL DEFAULT 'pending',
  requested_by    uuid         NOT NULL,
  requested_at    timestamptz  NOT NULL,
  decided_by      uuid,
  decided_at      timestamptz,
  decision_reason text,
  used_in_run     uuid,
  used_at         timestamptz,
  CONSTRAINT overtime_entry_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT overtime_entry_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT overtime_entry_by_fkey FOREIGN KEY (organisation_id, requested_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT overtime_entry_decided_by_fkey FOREIGN KEY (organisation_id, decided_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT overtime_entry_kind_known CHECK (kind IN ('overtime', 'rest_day', 'public_holiday')),
  CONSTRAINT overtime_entry_hours CHECK (hours > 0 AND hours <= 24),
  CONSTRAINT overtime_entry_branch CHECK ((kind = 'rest_day') = (branch IS NOT NULL) AND (branch IS NULL OR branch IN ('employer_request', 'employee_request'))),
  CONSTRAINT overtime_entry_state_known CHECK (state IN ('pending', 'approved', 'refused')),
  CONSTRAINT overtime_entry_decided_whole CHECK ((state = 'pending') = (decided_by IS NULL) AND (state = 'pending') = (decided_at IS NULL)),
  CONSTRAINT overtime_entry_refusal_reason CHECK (state <> 'refused' OR length(btrim(coalesce(decision_reason, ''))) > 0),
  CONSTRAINT overtime_entry_used_whole CHECK ((used_in_run IS NULL) = (used_at IS NULL) AND (used_in_run IS NULL OR state = 'approved')),
  CONSTRAINT overtime_entry_note_length CHECK (note IS NULL OR length(note) <= 500),
  CONSTRAINT overtime_entry_reason_length CHECK (decision_reason IS NULL OR length(decision_reason) <= 500)
);
-- One record of a kind for a person and a day while it is not refused.
CREATE UNIQUE INDEX overtime_entry_once ON nemura.overtime_entry (organisation_id, person_id, work_date, kind) WHERE state <> 'refused';
CREATE INDEX overtime_entry_state_idx ON nemura.overtime_entry (organisation_id, state, work_date);

-- +goose StatementBegin
CREATE FUNCTION nemura.overtime_entry_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'An overtime record is never deleted: work beyond the limit stays recorded' USING ERRCODE = 'NM153';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'pending' OR NEW.used_in_run IS NOT NULL THEN
      RAISE EXCEPTION 'An overtime record starts pending and unused' USING ERRCODE = 'NM153';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.work_date IS DISTINCT FROM OLD.work_date OR NEW.kind IS DISTINCT FROM OLD.kind OR NEW.hours IS DISTINCT FROM OLD.hours
     OR NEW.branch IS DISTINCT FROM OLD.branch OR NEW.over_limit IS DISTINCT FROM OLD.over_limit OR NEW.note IS DISTINCT FROM OLD.note
     OR NEW.requested_by IS DISTINCT FROM OLD.requested_by OR NEW.requested_at IS DISTINCT FROM OLD.requested_at THEN
    RAISE EXCEPTION 'An overtime record is evidence: only its decision and its use change' USING ERRCODE = 'NM153';
  END IF;
  IF NEW.state IS DISTINCT FROM OLD.state AND OLD.state <> 'pending' THEN
    RAISE EXCEPTION 'A % overtime record cannot become %', OLD.state, NEW.state USING ERRCODE = 'NM153';
  END IF;
  IF OLD.decided_by IS NOT NULL AND (NEW.decided_by IS DISTINCT FROM OLD.decided_by OR NEW.decided_at IS DISTINCT FROM OLD.decided_at OR NEW.decision_reason IS DISTINCT FROM OLD.decision_reason) THEN
    RAISE EXCEPTION 'A decision is written once' USING ERRCODE = 'NM153';
  END IF;
  IF OLD.used_in_run IS NOT NULL AND (NEW.used_in_run IS DISTINCT FROM OLD.used_in_run OR NEW.used_at IS DISTINCT FROM OLD.used_at) THEN
    RAISE EXCEPTION 'A record used by a pay run is used once' USING ERRCODE = 'NM153';
  END IF;
  IF NEW.state IN ('approved', 'refused') AND OLD.state = 'pending' AND EXISTS (SELECT 1 FROM nemura.person p WHERE p.organisation_id = NEW.organisation_id AND p.id = NEW.person_id AND p.member_id = NEW.decided_by) THEN
    RAISE EXCEPTION 'Nobody decides their own overtime' USING ERRCODE = 'NM154';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER overtime_entry_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.overtime_entry FOR EACH ROW EXECUTE FUNCTION nemura.overtime_entry_guard();

CREATE TABLE nemura.pay_input (
  organisation_id uuid         NOT NULL DEFAULT nemura.current_org(),
  id              uuid         NOT NULL DEFAULT uuidv7(),
  person_id       uuid         NOT NULL,
  kind            text         NOT NULL,
  days            numeric(8,2) NOT NULL,
  from_date       date         NOT NULL,
  to_date         date         NOT NULL,
  source_type     text         NOT NULL,   -- what it comes from: sick_notification, leave_ledger
  source_id       uuid         NOT NULL,
  note            text,
  created_by      uuid         NOT NULL,
  created_at      timestamptz  NOT NULL,
  used_in_run     uuid,
  used_at         timestamptz,
  CONSTRAINT pay_input_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pay_input_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT pay_input_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT pay_input_kind_known CHECK (kind IN ('unpaid_leave', 'encashment')),
  CONSTRAINT pay_input_days CHECK (days > 0),
  CONSTRAINT pay_input_dates CHECK (to_date >= from_date),
  CONSTRAINT pay_input_source_known CHECK (source_type IN ('sick_notification', 'leave_ledger')),
  CONSTRAINT pay_input_used_whole CHECK ((used_in_run IS NULL) = (used_at IS NULL)),
  CONSTRAINT pay_input_note_length CHECK (note IS NULL OR length(note) <= 500),
  CONSTRAINT pay_input_once_per_source UNIQUE (organisation_id, source_type, source_id, kind)
);
CREATE INDEX pay_input_open_idx ON nemura.pay_input (organisation_id, from_date) WHERE used_in_run IS NULL;

-- A pay input is written once; only its use by a pay run is added, once.
-- +goose StatementBegin
CREATE FUNCTION nemura.pay_input_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.kind IS DISTINCT FROM OLD.kind OR NEW.days IS DISTINCT FROM OLD.days OR NEW.from_date IS DISTINCT FROM OLD.from_date
     OR NEW.to_date IS DISTINCT FROM OLD.to_date OR NEW.source_type IS DISTINCT FROM OLD.source_type OR NEW.source_id IS DISTINCT FROM OLD.source_id OR NEW.note IS DISTINCT FROM OLD.note
     OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at IS DISTINCT FROM OLD.created_at OR (OLD.used_in_run IS NOT NULL AND (NEW.used_in_run IS DISTINCT FROM OLD.used_in_run OR NEW.used_at IS DISTINCT FROM OLD.used_at)) THEN
    RAISE EXCEPTION 'A pay input is evidence: it is written once and used once' USING ERRCODE = 'NM155';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER pay_input_guard BEFORE UPDATE OR DELETE ON nemura.pay_input FOR EACH ROW EXECUTE FUNCTION nemura.pay_input_guard();

-- How the days of a sick leave notification beyond the ceiling are handled: unpaid leave (a pay input) or taken from annual leave, decided once by the people team.
CREATE TABLE nemura.sick_unpaid_decision (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  notification_id uuid        NOT NULL,
  treatment       text        NOT NULL,
  decided_by      uuid        NOT NULL,
  decided_at      timestamptz NOT NULL,
  CONSTRAINT sick_unpaid_decision_pkey PRIMARY KEY (organisation_id, notification_id),
  CONSTRAINT sick_unpaid_decision_notification_fkey FOREIGN KEY (organisation_id, notification_id) REFERENCES nemura.sick_notification (organisation_id, id),
  CONSTRAINT sick_unpaid_decision_by_fkey FOREIGN KEY (organisation_id, decided_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT sick_unpaid_decision_treatment CHECK (treatment IN ('unpaid', 'annual'))
);
-- +goose StatementBegin
CREATE FUNCTION nemura.decision_once_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION '% is evidence: a decision is made once', TG_ARGV[0] USING ERRCODE = 'NM155';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER sick_unpaid_decision_guard BEFORE UPDATE OR DELETE ON nemura.sick_unpaid_decision FOR EACH ROW EXECUTE FUNCTION nemura.decision_once_guard('A decision on unpaid sick days');

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.overtime_entry DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('overtime_entry', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.pay_input DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('pay_input', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.sick_unpaid_decision DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('sick_unpaid_decision', 'notification_id');

ALTER TABLE nemura.overtime_entry ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.overtime_entry FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.overtime_entry USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.pay_input ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.pay_input FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.pay_input USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.sick_unpaid_decision ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.sick_unpaid_decision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.sick_unpaid_decision USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.overtime_entry, nemura.pay_input, nemura.sick_unpaid_decision TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, person_id, work_date, kind, hours, branch, over_limit, note, requested_by, requested_at) ON nemura.overtime_entry TO nemura_api;
GRANT UPDATE (state, decided_by, decided_at, decision_reason) ON nemura.overtime_entry TO nemura_api;
GRANT INSERT (id, person_id, kind, days, from_date, to_date, source_type, source_id, note, created_by, created_at) ON nemura.pay_input TO nemura_api;
GRANT INSERT (notification_id, treatment, decided_by, decided_at) ON nemura.sick_unpaid_decision TO nemura_api;
-- A pay run (a later change, with its own role) marks inputs used: through its own grant, never the API's.
