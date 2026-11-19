-- Cost rates per person and billing rates (PRD-001 FR-PRJ-06; p1-projects task 3.1, design D5; the owner's decision of 19 Nov
-- 2026, PRD-001 rev 0.295).
--
-- A person's cost rate is payroll data (FR-PAY-13): the Owner and the Payroll administrator enter and read it, through the API's
-- own Appendix D row. A rate is effective-dated and never changed: a new rate from a later day replaces it from that day, and the
-- rate of a day is the latest one starting on or before it. The role it names is the group a role-average rate is taken over (at
-- least three people, FR-PRJ-06). A billing rate is the price of an hour for a project, or for a customer's projects, for a
-- person, a role or everyone, in the project's currency, effective-dated the same way. Neither is ever updated or deleted (FY605).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.person_cost_rate (
  organisation_id uuid          NOT NULL DEFAULT fyniq.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  user_id         uuid,
  role_name       text          NOT NULL,
  effective_from  date          NOT NULL,
  hourly_rate     numeric(20,6) NOT NULL,
  currency        text          NOT NULL,
  minor_units     smallint      NOT NULL,
  created_by      text          NOT NULL,
  created_at      timestamptz   NOT NULL DEFAULT now(),
  CONSTRAINT person_cost_rate_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT person_cost_rate_day_key UNIQUE NULLS NOT DISTINCT (organisation_id, user_id, effective_from),
  CONSTRAINT person_cost_rate_user_fkey FOREIGN KEY (organisation_id, user_id) REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT person_cost_rate_currency_fkey FOREIGN KEY (currency, minor_units) REFERENCES fyniq.currency (code, minor_units),
  -- With no person it is the organisation's standard rate, the Owner's fallback for a viewer who may not see a person's own (design D5).
  CONSTRAINT person_cost_rate_standard CHECK (user_id IS NOT NULL OR role_name = 'Standard'),
  CONSTRAINT person_cost_rate_role_length CHECK (length(btrim(role_name)) BETWEEN 1 AND 80),
  CONSTRAINT person_cost_rate_amount CHECK (hourly_rate <> 'NaN' AND hourly_rate >= 0 AND hourly_rate = trunc(hourly_rate, 6))
);

CREATE TABLE fyniq.billing_rate (
  organisation_id uuid          NOT NULL DEFAULT fyniq.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  project_id      uuid,
  customer_id     uuid,
  user_id         uuid,
  role_name       text,
  effective_from  date          NOT NULL,
  hourly_rate     numeric(20,6) NOT NULL,
  currency        text          NOT NULL,
  minor_units     smallint      NOT NULL,
  created_by      text          NOT NULL,
  created_at      timestamptz   NOT NULL DEFAULT now(),
  CONSTRAINT billing_rate_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT billing_rate_project_fkey FOREIGN KEY (organisation_id, project_id) REFERENCES fyniq.project (organisation_id, id),
  CONSTRAINT billing_rate_customer_fkey FOREIGN KEY (organisation_id, customer_id) REFERENCES fyniq.contact (organisation_id, id),
  CONSTRAINT billing_rate_user_fkey FOREIGN KEY (organisation_id, user_id) REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT billing_rate_currency_fkey FOREIGN KEY (currency, minor_units) REFERENCES fyniq.currency (code, minor_units),
  -- For a project or for a customer's projects; for one person or one role, or for everyone.
  CONSTRAINT billing_rate_scope CHECK ((project_id IS NULL) <> (customer_id IS NULL) AND NOT (user_id IS NOT NULL AND role_name IS NOT NULL)),
  CONSTRAINT billing_rate_role_length CHECK (role_name IS NULL OR length(btrim(role_name)) BETWEEN 1 AND 80),
  CONSTRAINT billing_rate_amount CHECK (hourly_rate <> 'NaN' AND hourly_rate >= 0 AND hourly_rate = trunc(hourly_rate, 6))
);
CREATE UNIQUE INDEX billing_rate_day_key ON fyniq.billing_rate (organisation_id, COALESCE(project_id, customer_id), COALESCE(user_id, '00000000-0000-0000-0000-000000000000'::uuid),
  COALESCE(role_name, ''), effective_from);

-- +goose StatementBegin
-- A rate is never changed or deleted, by any role: a later day's rate replaces it from that day (FY605).
CREATE FUNCTION fyniq.rate_immutable() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A rate never changes or goes: enter the new rate from the day it starts' USING ERRCODE = 'FY605';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER person_cost_rate_immutable BEFORE UPDATE OR DELETE ON fyniq.person_cost_rate FOR EACH ROW EXECUTE FUNCTION fyniq.rate_immutable();
CREATE TRIGGER billing_rate_immutable BEFORE UPDATE OR DELETE ON fyniq.billing_rate FOR EACH ROW EXECUTE FUNCTION fyniq.rate_immutable();
CREATE TRIGGER person_cost_rate_no_truncate BEFORE TRUNCATE ON fyniq.person_cost_rate FOR EACH STATEMENT EXECUTE FUNCTION fyniq.rate_immutable();
CREATE TRIGGER billing_rate_no_truncate BEFORE TRUNCATE ON fyniq.billing_rate FOR EACH STATEMENT EXECUTE FUNCTION fyniq.rate_immutable();

ALTER TABLE fyniq.person_cost_rate ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.person_cost_rate FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.person_cost_rate
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.billing_rate ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.billing_rate FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.billing_rate
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.person_cost_rate
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('person_cost_rate', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.billing_rate
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('billing_rate', 'id');

-- The AI role reads neither: a person's cost rate never reaches a model (G4-050).
GRANT SELECT ON fyniq.person_cost_rate, fyniq.billing_rate TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (user_id, role_name, effective_from, hourly_rate, currency, minor_units, created_by) ON fyniq.person_cost_rate TO fyniq_api;
GRANT INSERT (project_id, customer_id, user_id, role_name, effective_from, hourly_rate, currency, minor_units, created_by) ON fyniq.billing_rate TO fyniq_api;
