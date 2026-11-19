-- A person's key employment terms (PRD-002 NM-LTR-06; p1-nemura-self-service-letters slice 2).
--
-- What the Employment Act's key employment terms need that the people record does not hold: the main duties, the length of a fixed-term contract, the salary period, the basic
-- salary and fixed allowances and deductions, the overtime payment period, other pay, medical benefits and the notice period. Effective-dated history that is only ever ended,
-- once. The pay terms are written by the Payroll administrator or the Owner and read by those who issue the KETs (owner decision O3, 20 Nov 2026).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.person_terms (
  organisation_id  uuid          NOT NULL DEFAULT nemura.current_org(),
  id               uuid          NOT NULL DEFAULT uuidv7(),
  person_id        uuid          NOT NULL,
  effective_from   date          NOT NULL,
  effective_to     date,
  main_duties      text          NOT NULL,
  contract_end     date,                       -- a fixed-term contract's last day
  salary_period    text,
  basic_salary     numeric(20,4),              -- SGD per salary period
  fixed_allowances text,
  fixed_deductions text,
  overtime_period  text,                       -- only when it differs from the salary period
  other_pay        text,
  medical_benefits text,
  notice_period    text          NOT NULL,
  created_by       uuid          NOT NULL,
  created_at       timestamptz   NOT NULL,
  CONSTRAINT person_terms_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT person_terms_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT person_terms_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT person_terms_dates CHECK (effective_to IS NULL OR effective_to > effective_from),
  CONSTRAINT person_terms_contract_end CHECK (contract_end IS NULL OR contract_end >= effective_from),
  CONSTRAINT person_terms_period_known CHECK (salary_period IS NULL OR salary_period IN ('hourly', 'daily', 'weekly', 'fortnightly', 'monthly')),
  CONSTRAINT person_terms_salary CHECK (basic_salary IS NULL OR (basic_salary > 0 AND basic_salary = round(basic_salary, 2))),
  CONSTRAINT person_terms_pay_whole CHECK ((salary_period IS NULL) = (basic_salary IS NULL)),
  CONSTRAINT person_terms_lengths CHECK (length(btrim(main_duties)) BETWEEN 1 AND 1000 AND length(btrim(notice_period)) BETWEEN 1 AND 300
    AND coalesce(length(fixed_allowances), 0) <= 500 AND coalesce(length(fixed_deductions), 0) <= 500 AND coalesce(length(overtime_period), 0) <= 200
    AND coalesce(length(other_pay), 0) <= 500 AND coalesce(length(medical_benefits), 0) <= 500)
);
CREATE INDEX person_terms_person_idx ON nemura.person_terms (organisation_id, person_id, effective_from);

-- +goose StatementBegin
CREATE FUNCTION nemura.person_terms_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (TG_OP = 'UPDATE' AND (NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.effective_from IS DISTINCT FROM OLD.effective_from OR NEW.main_duties IS DISTINCT FROM OLD.main_duties
     OR NEW.contract_end IS DISTINCT FROM OLD.contract_end OR NEW.salary_period IS DISTINCT FROM OLD.salary_period OR NEW.basic_salary IS DISTINCT FROM OLD.basic_salary
     OR NEW.fixed_allowances IS DISTINCT FROM OLD.fixed_allowances OR NEW.fixed_deductions IS DISTINCT FROM OLD.fixed_deductions OR NEW.overtime_period IS DISTINCT FROM OLD.overtime_period
     OR NEW.other_pay IS DISTINCT FROM OLD.other_pay OR NEW.medical_benefits IS DISTINCT FROM OLD.medical_benefits OR NEW.notice_period IS DISTINCT FROM OLD.notice_period
     OR (OLD.effective_to IS NOT NULL AND NEW.effective_to IS DISTINCT FROM OLD.effective_to))) THEN
    RAISE EXCEPTION 'A record of terms is history: it can only be ended, once' USING ERRCODE = 'NM163';
  END IF;
  IF EXISTS (SELECT 1 FROM nemura.person_terms j WHERE j.organisation_id = NEW.organisation_id AND j.person_id = NEW.person_id AND j.id <> NEW.id
               AND daterange(j.effective_from, coalesce(j.effective_to, 'infinity'), '[)') && daterange(NEW.effective_from, coalesce(NEW.effective_to, 'infinity'), '[)')) THEN
    RAISE EXCEPTION 'A person has one record of terms at a time' USING ERRCODE = 'NM164';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER person_terms_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.person_terms FOR EACH ROW EXECUTE FUNCTION nemura.person_terms_guard();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.person_terms DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('person_terms', 'id');

ALTER TABLE nemura.person_terms ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.person_terms FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.person_terms USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.person_terms TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, person_id, effective_from, main_duties, contract_end, salary_period, basic_salary, fixed_allowances, fixed_deductions, overtime_period, other_pay, medical_benefits, notice_period, created_by, created_at),
  UPDATE (effective_to) ON nemura.person_terms TO nemura_api;
