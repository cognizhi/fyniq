-- The month-end leave liability accrual, as a draft for approval (PRD-002 NM-LV-09; p1-nemura-leave-time slice 4).
--
-- For each person, the carried-forward annual leave still untaken at a month's end. The amounts come from the pay structure (payroll, a later change) and the journal
-- from the Fyniq link: Nemura keeps the days and who approved them, and never posts anything. A month is drafted once; a draft is approved once, by someone other than
-- the person who drafted it; nothing is edited or deleted.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.leave_accrual (
  organisation_id uuid         NOT NULL DEFAULT nemura.current_org(),
  id              uuid         NOT NULL DEFAULT uuidv7(),
  month           date         NOT NULL,   -- the first day of the month
  person_id       uuid         NOT NULL,
  carried_days    numeric(8,2) NOT NULL,   -- carried-forward annual leave untaken at the month's end
  annual_left     numeric(8,2) NOT NULL,   -- all annual leave left, for the accountant's information
  state           text         NOT NULL DEFAULT 'draft',
  drafted_by      uuid         NOT NULL,
  drafted_at      timestamptz  NOT NULL,
  approved_by     uuid,
  approved_at     timestamptz,
  CONSTRAINT leave_accrual_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT leave_accrual_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT leave_accrual_drafted_by_fkey FOREIGN KEY (organisation_id, drafted_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT leave_accrual_approved_by_fkey FOREIGN KEY (organisation_id, approved_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT leave_accrual_month_first CHECK (extract(day FROM month) = 1),
  CONSTRAINT leave_accrual_days CHECK (carried_days >= 0 AND annual_left >= 0),
  CONSTRAINT leave_accrual_state_known CHECK (state IN ('draft', 'approved')),
  CONSTRAINT leave_accrual_approved_whole CHECK ((state = 'approved') = (approved_by IS NOT NULL) AND (state = 'approved') = (approved_at IS NOT NULL)),
  CONSTRAINT leave_accrual_not_own_approval CHECK (approved_by IS NULL OR approved_by <> drafted_by),
  CONSTRAINT leave_accrual_once UNIQUE (organisation_id, month, person_id)
);

-- +goose StatementBegin
CREATE FUNCTION nemura.leave_accrual_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.month IS DISTINCT FROM OLD.month OR NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.carried_days IS DISTINCT FROM OLD.carried_days
     OR NEW.annual_left IS DISTINCT FROM OLD.annual_left OR NEW.drafted_by IS DISTINCT FROM OLD.drafted_by OR NEW.drafted_at IS DISTINCT FROM OLD.drafted_at
     OR (OLD.state = 'approved' AND NEW IS DISTINCT FROM OLD) THEN
    RAISE EXCEPTION 'An accrual draft is evidence: it is approved once and never edited or deleted' USING ERRCODE = 'NM156';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER leave_accrual_guard BEFORE UPDATE OR DELETE ON nemura.leave_accrual FOR EACH ROW EXECUTE FUNCTION nemura.leave_accrual_guard();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.leave_accrual DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('leave_accrual', 'id');

ALTER TABLE nemura.leave_accrual ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.leave_accrual FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.leave_accrual USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.leave_accrual TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, month, person_id, carried_days, annual_left, drafted_by, drafted_at) ON nemura.leave_accrual TO nemura_api;
GRANT UPDATE (state, approved_by, approved_at) ON nemura.leave_accrual TO nemura_api;
