-- Working patterns and the leave ledger (PRD-002 NM-TIM-01, NM-LV-02, NM-LV-11, NM-LV-13; p1-nemura-leave-time slice 1).
--
-- A working pattern says which weekdays a person works and how many hours: leave counts days by it. The leave ledger is the balance: every change is an entry
-- (grant, accrual, reservation, use, release, expiry, encashment, transfer, correction) with its leave year and source, append-only; a balance is a sum of
-- entries. The balance check and the reservation happen in one transaction: the head row of a person's type and year is locked first, so two requests cannot
-- both take the last day (design D1, D2).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.working_pattern (
  organisation_id uuid         NOT NULL DEFAULT nemura.current_org(),
  id              uuid         NOT NULL DEFAULT uuidv7(),
  person_id       uuid         NOT NULL,
  effective_from  date         NOT NULL,
  effective_to    date,
  weekdays        smallint[]   NOT NULL,   -- ISO weekdays worked: 1 Monday to 7 Sunday
  hours_per_week  numeric(4,1) NOT NULL,
  created_at      timestamptz  NOT NULL DEFAULT now(),
  CONSTRAINT working_pattern_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT working_pattern_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT working_pattern_dates CHECK (effective_to IS NULL OR effective_to > effective_from),
  CONSTRAINT working_pattern_weekdays CHECK (cardinality(weekdays) BETWEEN 1 AND 7 AND weekdays <@ ARRAY[1, 2, 3, 4, 5, 6, 7]::smallint[]),
  CONSTRAINT working_pattern_hours CHECK (hours_per_week > 0 AND hours_per_week <= 168)
);
CREATE INDEX working_pattern_person_idx ON nemura.working_pattern (organisation_id, person_id, effective_from);

-- +goose StatementBegin
CREATE FUNCTION nemura.working_pattern_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (TG_OP = 'UPDATE' AND (NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.effective_from IS DISTINCT FROM OLD.effective_from
     OR NEW.weekdays IS DISTINCT FROM OLD.weekdays OR NEW.hours_per_week IS DISTINCT FROM OLD.hours_per_week
     OR (OLD.effective_to IS NOT NULL AND NEW.effective_to IS DISTINCT FROM OLD.effective_to))) THEN
    RAISE EXCEPTION 'A working pattern is history: it can only be ended, once' USING ERRCODE = 'NM140';
  END IF;
  IF EXISTS (SELECT 1 FROM nemura.working_pattern w WHERE w.organisation_id = NEW.organisation_id AND w.person_id = NEW.person_id AND w.id <> NEW.id
               AND daterange(w.effective_from, coalesce(w.effective_to, 'infinity'), '[)') && daterange(NEW.effective_from, coalesce(NEW.effective_to, 'infinity'), '[)')) THEN
    RAISE EXCEPTION 'A person has one working pattern at a time' USING ERRCODE = 'NM141';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER working_pattern_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.working_pattern FOR EACH ROW EXECUTE FUNCTION nemura.working_pattern_guard();

-- The lock of a person's balance of one type in one leave year: a request locks it, reads the balance, and writes its reservation in the same transaction.
CREATE TABLE nemura.leave_head (
  organisation_id uuid   NOT NULL DEFAULT nemura.current_org(),
  person_id       uuid   NOT NULL,
  type_key        text   NOT NULL,
  leave_year      integer NOT NULL,
  CONSTRAINT leave_head_pkey PRIMARY KEY (organisation_id, person_id, type_key, leave_year),
  CONSTRAINT leave_head_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT leave_head_type_key CHECK (type_key ~ '^[a-z][a-z0-9_.]{1,60}$'),
  CONSTRAINT leave_head_year CHECK (leave_year BETWEEN 2000 AND 2200)
);

CREATE TABLE nemura.leave_ledger (
  organisation_id uuid         NOT NULL DEFAULT nemura.current_org(),
  id              uuid         NOT NULL DEFAULT uuidv7(),
  person_id       uuid         NOT NULL,
  type_key        text         NOT NULL,
  leave_year      integer      NOT NULL,
  kind            text         NOT NULL,
  amount          numeric(8,2) NOT NULL,   -- in days, signed: a grant adds, a use takes
  negative_ok     boolean      NOT NULL DEFAULT false,
  expires_on      date,                    -- a carried-forward grant's last day
  request_id      uuid,
  source          text         NOT NULL,   -- where it comes from: a pack rule, a policy row, a request, a person
  note            text,
  created_by      uuid         NOT NULL,
  created_at      timestamptz  NOT NULL DEFAULT now(),
  CONSTRAINT leave_ledger_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT leave_ledger_head_fkey FOREIGN KEY (organisation_id, person_id, type_key, leave_year) REFERENCES nemura.leave_head (organisation_id, person_id, type_key, leave_year),
  CONSTRAINT leave_ledger_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT leave_ledger_kind_known CHECK (kind IN ('grant', 'accrual', 'reservation', 'use', 'release', 'expiry', 'encashment', 'transfer', 'correction')),
  CONSTRAINT leave_ledger_amount_nonzero CHECK (amount <> 0),
  CONSTRAINT leave_ledger_sign CHECK ((kind IN ('grant', 'accrual', 'release') AND amount > 0) OR (kind IN ('reservation', 'use', 'expiry', 'encashment') AND amount < 0) OR kind IN ('transfer', 'correction')),
  CONSTRAINT leave_ledger_source_length CHECK (length(btrim(source)) BETWEEN 1 AND 200),
  CONSTRAINT leave_ledger_note_length CHECK (note IS NULL OR length(note) <= 500)
);
CREATE INDEX leave_ledger_person_idx ON nemura.leave_ledger (organisation_id, person_id, type_key, leave_year, created_at);

-- The ledger is append-only, and an entry that would take a type that forbids it below zero is refused: the balance is checked against the head's lock.
-- +goose StatementBegin
CREATE FUNCTION nemura.leave_ledger_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  total numeric;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The leave ledger is append-only: a correction is a new entry' USING ERRCODE = 'NM142';
  END IF;
  IF NOT NEW.negative_ok AND NEW.amount < 0 THEN
    SELECT coalesce(sum(amount), 0) INTO total FROM nemura.leave_ledger
     WHERE organisation_id = NEW.organisation_id AND person_id = NEW.person_id AND type_key = NEW.type_key AND leave_year = NEW.leave_year;
    IF total + NEW.amount < 0 THEN
      RAISE EXCEPTION 'That would take the balance below zero' USING ERRCODE = 'NM143';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER leave_ledger_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.leave_ledger FOR EACH ROW EXECUTE FUNCTION nemura.leave_ledger_guard();
CREATE TRIGGER leave_ledger_no_truncate BEFORE TRUNCATE ON nemura.leave_ledger FOR EACH STATEMENT EXECUTE FUNCTION nemura.leave_ledger_guard();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.working_pattern DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('working_pattern', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.leave_ledger DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('leave_ledger', 'id');

ALTER TABLE nemura.working_pattern ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.working_pattern FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.working_pattern USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.leave_head ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.leave_head FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.leave_head USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.leave_ledger ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.leave_ledger FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.leave_ledger USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.working_pattern, nemura.leave_head, nemura.leave_ledger TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, person_id, effective_from, weekdays, hours_per_week), UPDATE (effective_to) ON nemura.working_pattern TO nemura_api;
GRANT INSERT (person_id, type_key, leave_year) ON nemura.leave_head TO nemura_api;
GRANT INSERT (id, person_id, type_key, leave_year, kind, amount, negative_ok, expires_on, request_id, source, note, created_by) ON nemura.leave_ledger TO nemura_api;
-- The worker writes none yet: the year-end carry-forward and the month-end accrual arrive as jobs with their own grants (slice 4).
-- The lock of the head: SELECT ... FOR UPDATE needs an UPDATE privilege on one column, which a head row has none to give; the lock is taken through this function.
-- +goose StatementBegin
CREATE FUNCTION nemura.lock_leave_head(p_person uuid, p_type text, p_year integer) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  INSERT INTO nemura.leave_head (person_id, type_key, leave_year) VALUES (p_person, p_type, p_year) ON CONFLICT DO NOTHING;
  PERFORM 1 FROM nemura.leave_head WHERE organisation_id = nemura.current_org() AND person_id = p_person AND type_key = p_type AND leave_year = p_year FOR UPDATE;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION nemura.lock_leave_head(uuid, text, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.lock_leave_head(uuid, text, integer) TO nemura_api;
