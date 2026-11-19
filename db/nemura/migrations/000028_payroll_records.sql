-- Pay records (PRD-001 FR-EMP-01, FR-PAY-05, FR-PAY-14; PRD-002 NM-PAY-02, NM-PAY-03; p1-nemura-payroll slice 2).
--
-- A pay group (its pay day and the cut-offs), a person's pay as effective-dated history that is only ever ended (the group, the monthly basic salary, the self-help group fund, the
-- recurring allowances and fixed deductions with the facts that class them), the permanent resident's joint election with its evidence, the year to date of CPF wages imported at
-- cut-over, and the one-off items of a month. Pay is the most restricted data Nemura holds: the API reads and writes it only for the Payroll administrator and the Owner, and no audit
-- event holds an amount.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.pay_group (
  organisation_id  uuid        NOT NULL DEFAULT nemura.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  name             text        NOT NULL,
  pay_day          integer     NOT NULL,   -- the day of the month salary is paid; 0 is the last day of the month
  cutoff_days      integer     NOT NULL,   -- inputs are accepted until this many days before the pay day
  approval_days    integer     NOT NULL,   -- the run is approved at the latest this many days before the pay day
  created_by       uuid        NOT NULL,
  created_at       timestamptz NOT NULL,
  CONSTRAINT pay_group_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pay_group_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT pay_group_name_once UNIQUE (organisation_id, name),
  CONSTRAINT pay_group_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 80),
  CONSTRAINT pay_group_pay_day CHECK (pay_day BETWEEN 0 AND 28),
  CONSTRAINT pay_group_cutoffs CHECK (cutoff_days BETWEEN 0 AND 20 AND approval_days BETWEEN 0 AND 20 AND approval_days <= cutoff_days)
);

CREATE TABLE nemura.person_pay (
  organisation_id  uuid          NOT NULL DEFAULT nemura.current_org(),
  id               uuid          NOT NULL DEFAULT uuidv7(),
  person_id        uuid          NOT NULL,
  effective_from   date          NOT NULL,
  effective_to     date,
  pay_group_id     uuid          NOT NULL,
  basic_monthly    numeric(20,4) NOT NULL,
  shg_fund         text,
  shg_opt_out      boolean       NOT NULL DEFAULT false,
  created_by       uuid          NOT NULL,
  created_at       timestamptz   NOT NULL,
  CONSTRAINT person_pay_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT person_pay_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT person_pay_group_fkey FOREIGN KEY (organisation_id, pay_group_id) REFERENCES nemura.pay_group (organisation_id, id),
  CONSTRAINT person_pay_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT person_pay_dates CHECK (effective_to IS NULL OR effective_to > effective_from),
  CONSTRAINT person_pay_basic CHECK (basic_monthly >= 0 AND basic_monthly = round(basic_monthly, 2)),
  CONSTRAINT person_pay_fund_known CHECK (shg_fund IS NULL OR shg_fund IN ('CDAC', 'ECF', 'MBMF', 'SINDA'))
);
CREATE INDEX person_pay_person_idx ON nemura.person_pay (organisation_id, person_id, effective_from);

-- The recurring allowances and fixed deductions of one pay record, with the facts that class them (FR-PAY-17): whether they are CPF wages and what they are.
CREATE TABLE nemura.person_pay_item (
  organisation_id  uuid          NOT NULL DEFAULT nemura.current_org(),
  id               uuid          NOT NULL DEFAULT uuidv7(),
  pay_id           uuid          NOT NULL,
  key              text          NOT NULL,
  name             text          NOT NULL,
  kind             text          NOT NULL,
  nature           text          NOT NULL,
  amount           numeric(20,4) NOT NULL,
  cpf_wages        boolean       NOT NULL,
  CONSTRAINT person_pay_item_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT person_pay_item_pay_fkey FOREIGN KEY (organisation_id, pay_id) REFERENCES nemura.person_pay (organisation_id, id),
  CONSTRAINT person_pay_item_key_once UNIQUE (organisation_id, pay_id, key),
  CONSTRAINT person_pay_item_kind_known CHECK (kind IN ('allowance', 'fixed_deduction')),
  CONSTRAINT person_pay_item_nature_known CHECK (nature IN ('allowance', 'overtime', 'other', 'accommodation', 'cooperative', 'other_deduction')),
  CONSTRAINT person_pay_item_amount CHECK (amount > 0 AND amount = round(amount, 2)),
  CONSTRAINT person_pay_item_deduction_not_wages CHECK (kind = 'allowance' OR NOT cpf_wages),
  CONSTRAINT person_pay_item_text CHECK (length(btrim(key)) BETWEEN 1 AND 40 AND length(btrim(name)) BETWEEN 1 AND 80)
);

-- A joint election to contribute at full employer rates in the first two years of permanent residence, with the evidence (the CPF Board's approval of the application).
CREATE TABLE nemura.pr_election (
  organisation_id  uuid        NOT NULL DEFAULT nemura.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  person_id        uuid        NOT NULL,
  effective_from   date        NOT NULL,
  effective_to     date,
  evidence_id      uuid        NOT NULL,
  created_by       uuid        NOT NULL,
  created_at       timestamptz NOT NULL,
  CONSTRAINT pr_election_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pr_election_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT pr_election_evidence_fkey FOREIGN KEY (organisation_id, evidence_id) REFERENCES nemura.person_document (organisation_id, id),
  CONSTRAINT pr_election_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT pr_election_dates CHECK (effective_to IS NULL OR effective_to > effective_from)
);

-- The year to date of CPF wages for this employer: imported at cut-over for a company that starts mid-year, and added to by each approved run (FR-PAY-14).
CREATE TABLE nemura.cpf_ytd (
  organisation_id  uuid          NOT NULL DEFAULT nemura.current_org(),
  person_id        uuid          NOT NULL,
  year             integer       NOT NULL,
  ow               numeric(20,4) NOT NULL,   -- ordinary wages subject to CPF
  aw_subject       numeric(20,4) NOT NULL,   -- additional wages subject to CPF
  aw_paid          numeric(20,4) NOT NULL,   -- additional wages paid, subject or not
  through_month    date          NOT NULL,   -- the first day of the last month counted
  source           text          NOT NULL,
  updated_by       uuid          NOT NULL,
  updated_at       timestamptz   NOT NULL,
  CONSTRAINT cpf_ytd_pkey PRIMARY KEY (organisation_id, person_id, year),
  CONSTRAINT cpf_ytd_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT cpf_ytd_by_fkey FOREIGN KEY (organisation_id, updated_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT cpf_ytd_amounts CHECK (ow >= 0 AND aw_subject >= 0 AND aw_paid >= 0 AND aw_subject <= aw_paid),
  CONSTRAINT cpf_ytd_source_known CHECK (source IN ('import', 'run')),
  CONSTRAINT cpf_ytd_month CHECK (extract(day FROM through_month) = 1 AND extract(year FROM through_month) = year)
);

-- The one-off items of a month: a bonus, an allowance, a commission, with the facts that class them (FR-PAY-17). Each is used by one run once.
CREATE TABLE nemura.pay_one_off (
  organisation_id  uuid          NOT NULL DEFAULT nemura.current_org(),
  id               uuid          NOT NULL DEFAULT uuidv7(),
  person_id        uuid          NOT NULL,
  month            date          NOT NULL,   -- the wage month it is paid in: the first day
  key              text          NOT NULL,
  name             text          NOT NULL,
  nature           text          NOT NULL,
  amount           numeric(20,4) NOT NULL,
  cpf_wages        boolean       NOT NULL,
  for_month        date,
  payable_on       date,
  note             text,
  created_by       uuid          NOT NULL,
  created_at       timestamptz   NOT NULL,
  used_in_run      uuid,
  used_at          timestamptz,
  CONSTRAINT pay_one_off_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pay_one_off_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT pay_one_off_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT pay_one_off_month CHECK (extract(day FROM month) = 1),
  CONSTRAINT pay_one_off_for_month CHECK (for_month IS NULL OR extract(day FROM for_month) = 1),
  CONSTRAINT pay_one_off_nature_known CHECK (nature IN ('bonus', 'commission', 'allowance', 'other')),
  CONSTRAINT pay_one_off_amount CHECK (amount > 0 AND amount = round(amount, 2)),
  CONSTRAINT pay_one_off_key_once UNIQUE (organisation_id, person_id, month, key),
  CONSTRAINT pay_one_off_used_whole CHECK ((used_in_run IS NULL) = (used_at IS NULL)),
  CONSTRAINT pay_one_off_text CHECK (length(btrim(key)) BETWEEN 1 AND 40 AND length(btrim(name)) BETWEEN 1 AND 80 AND coalesce(length(note), 0) <= 300)
);
CREATE INDEX pay_one_off_month_idx ON nemura.pay_one_off (organisation_id, month);

-- A person's pay history and an election are history that is only ended, once; a one-off that a run used never changes. One guard for each table: a plpgsql expression is not
-- short-circuited, so a field that only one table has cannot be named in a shared function.
-- +goose StatementBegin
CREATE FUNCTION nemura.person_pay_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.effective_from IS DISTINCT FROM OLD.effective_from OR NEW.pay_group_id IS DISTINCT FROM OLD.pay_group_id
     OR NEW.basic_monthly IS DISTINCT FROM OLD.basic_monthly OR NEW.shg_fund IS DISTINCT FROM OLD.shg_fund OR NEW.shg_opt_out IS DISTINCT FROM OLD.shg_opt_out
     OR (OLD.effective_to IS NOT NULL AND NEW.effective_to IS DISTINCT FROM OLD.effective_to) THEN
    RAISE EXCEPTION 'A pay record is history: it can only be ended, once' USING ERRCODE = 'NM171';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER person_pay_guard BEFORE UPDATE OR DELETE ON nemura.person_pay FOR EACH ROW EXECUTE FUNCTION nemura.person_pay_guard();

-- +goose StatementBegin
CREATE FUNCTION nemura.pr_election_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.effective_from IS DISTINCT FROM OLD.effective_from OR NEW.evidence_id IS DISTINCT FROM OLD.evidence_id
     OR (OLD.effective_to IS NOT NULL AND NEW.effective_to IS DISTINCT FROM OLD.effective_to) THEN
    RAISE EXCEPTION 'An election is history: it can only be ended, once' USING ERRCODE = 'NM171';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER pr_election_guard BEFORE UPDATE OR DELETE ON nemura.pr_election FOR EACH ROW EXECUTE FUNCTION nemura.pr_election_guard();

-- +goose StatementBegin
CREATE FUNCTION nemura.pay_one_off_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.month IS DISTINCT FROM OLD.month OR NEW.amount IS DISTINCT FROM OLD.amount OR NEW.name IS DISTINCT FROM OLD.name
     OR NEW.nature IS DISTINCT FROM OLD.nature OR NEW.cpf_wages IS DISTINCT FROM OLD.cpf_wages OR NEW.for_month IS DISTINCT FROM OLD.for_month OR NEW.payable_on IS DISTINCT FROM OLD.payable_on
     OR (OLD.used_in_run IS NOT NULL AND NEW.used_in_run IS DISTINCT FROM OLD.used_in_run) THEN
    RAISE EXCEPTION 'A one-off item keeps its words and its figures, and is used by one run once' USING ERRCODE = 'NM171';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER pay_one_off_guard BEFORE UPDATE OR DELETE ON nemura.pay_one_off FOR EACH ROW EXECUTE FUNCTION nemura.pay_one_off_guard();
CREATE TRIGGER person_pay_item_append_only BEFORE UPDATE OR DELETE ON nemura.person_pay_item FOR EACH ROW EXECUTE FUNCTION nemura.append_only_guard('A pay item');

-- No two pay records of one person overlap.
-- +goose StatementBegin
CREATE FUNCTION nemura.person_pay_overlap() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM nemura.person_pay p WHERE p.organisation_id = NEW.organisation_id AND p.person_id = NEW.person_id AND p.id <> NEW.id
               AND daterange(p.effective_from, coalesce(p.effective_to, 'infinity'), '[)') && daterange(NEW.effective_from, coalesce(NEW.effective_to, 'infinity'), '[)')) THEN
    RAISE EXCEPTION 'A person has one pay record at a time' USING ERRCODE = 'NM172';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER person_pay_overlap BEFORE INSERT OR UPDATE ON nemura.person_pay FOR EACH ROW EXECUTE FUNCTION nemura.person_pay_overlap();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.pay_group DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('pay_group', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.person_pay DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('person_pay', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.pr_election DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('pr_election', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.cpf_ytd DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('cpf_ytd', 'person_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.pay_one_off DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('pay_one_off', 'id');

ALTER TABLE nemura.pay_group ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.pay_group FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.pay_group USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.person_pay ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.person_pay FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.person_pay USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.person_pay_item ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.person_pay_item FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.person_pay_item USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.pr_election ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.pr_election FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.pr_election USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.cpf_ytd ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.cpf_ytd FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.cpf_ytd USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.pay_one_off ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.pay_one_off FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.pay_one_off USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.pay_group, nemura.person_pay, nemura.person_pay_item, nemura.pr_election, nemura.cpf_ytd, nemura.pay_one_off TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, name, pay_day, cutoff_days, approval_days, created_by, created_at) ON nemura.pay_group TO nemura_api;
GRANT INSERT (id, person_id, effective_from, pay_group_id, basic_monthly, shg_fund, shg_opt_out, created_by, created_at), UPDATE (effective_to) ON nemura.person_pay TO nemura_api;
GRANT INSERT (id, pay_id, key, name, kind, nature, amount, cpf_wages) ON nemura.person_pay_item TO nemura_api;
GRANT INSERT (id, person_id, effective_from, evidence_id, created_by, created_at), UPDATE (effective_to) ON nemura.pr_election TO nemura_api;
GRANT INSERT (person_id, year, ow, aw_subject, aw_paid, through_month, source, updated_by, updated_at), UPDATE (ow, aw_subject, aw_paid, through_month, source, updated_by, updated_at) ON nemura.cpf_ytd TO nemura_api;
GRANT INSERT (id, person_id, month, key, name, nature, amount, cpf_wages, for_month, payable_on, note, created_by, created_at), UPDATE (used_in_run, used_at) ON nemura.pay_one_off TO nemura_api;
