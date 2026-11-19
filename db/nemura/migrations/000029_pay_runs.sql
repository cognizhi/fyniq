-- Pay runs (PRD-001 FR-PAY-01, 06, 07; PRD-002 NM-PAY-09, NM-PAY-10; p1-nemura-payroll slice 3).
--
-- A run is one calendar month of one pay group. Every calculation of it is a version that is read-only once written: a change (an override, a correction) is the next version, which
-- states the full corrected figures of its month and names the version it supersedes. A version is approved by someone other than the person who prepared it (a constraint, not only
-- the API), confirming its digest with a passkey. The lines are evidence: written once, never changed or deleted. No audit event holds an amount.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.pay_run (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  month           date        NOT NULL,
  pay_group_id    uuid        NOT NULL,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  CONSTRAINT pay_run_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pay_run_group_fkey FOREIGN KEY (organisation_id, pay_group_id) REFERENCES nemura.pay_group (organisation_id, id),
  CONSTRAINT pay_run_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT pay_run_month CHECK (extract(day FROM month) = 1),
  CONSTRAINT pay_run_once UNIQUE (organisation_id, month, pay_group_id)
);

CREATE TABLE nemura.pay_run_version (
  organisation_id uuid          NOT NULL DEFAULT nemura.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  run_id          uuid          NOT NULL,
  version         integer       NOT NULL,
  state           text          NOT NULL DEFAULT 'calculated',
  prepared_by     uuid          NOT NULL,
  prepared_at     timestamptz   NOT NULL,
  supersedes      uuid,
  reason          text,
  digest          bytea         NOT NULL,   -- SHA-256 of the version's figures: what an approver confirms
  pack_versions   text[]        NOT NULL,
  people          integer       NOT NULL,
  flags_failing   integer       NOT NULL,
  flags_warning   integer       NOT NULL,
  gross           numeric(20,4) NOT NULL,
  cpf_employee    numeric(20,4) NOT NULL,
  cpf_employer    numeric(20,4) NOT NULL,
  sdl_payable     numeric(20,4) NOT NULL,
  shg             numeric(20,4) NOT NULL,
  deductions      numeric(20,4) NOT NULL,
  reimbursements  numeric(20,4) NOT NULL,
  net             numeric(20,4) NOT NULL,
  disburse        numeric(20,4) NOT NULL,
  employer_cost   numeric(20,4) NOT NULL,
  approved_by     uuid,
  approved_at     timestamptz,
  approved_digest bytea,
  envelope        jsonb,                    -- what the approver's passkey confirmed: the run, the version, the digest and when
  CONSTRAINT pay_run_version_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pay_run_version_run_fkey FOREIGN KEY (organisation_id, run_id) REFERENCES nemura.pay_run (organisation_id, id),
  CONSTRAINT pay_run_version_by_fkey FOREIGN KEY (organisation_id, prepared_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT pay_run_version_approver_fkey FOREIGN KEY (organisation_id, approved_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT pay_run_version_supersedes_fkey FOREIGN KEY (organisation_id, supersedes) REFERENCES nemura.pay_run_version (organisation_id, id),
  CONSTRAINT pay_run_version_once UNIQUE (organisation_id, run_id, version),
  CONSTRAINT pay_run_version_state CHECK (state IN ('calculated', 'approved', 'superseded', 'posted', 'paid_in_part', 'paid', 'closed')),
  CONSTRAINT pay_run_version_digest CHECK (octet_length(digest) = 32),
  CONSTRAINT pay_run_version_positive CHECK (version >= 1 AND people >= 0 AND flags_failing >= 0 AND flags_warning >= 0),
  CONSTRAINT pay_run_version_correction CHECK ((version = 1) = (supersedes IS NULL)),
  -- nobody approves the run they prepared (SOD-4, NM-ROLE-04): a constraint, so no path around the API can
  CONSTRAINT pay_run_version_segregation CHECK (approved_by IS NULL OR approved_by <> prepared_by),
  CONSTRAINT pay_run_version_approval_whole CHECK ((approved_by IS NULL) = (approved_at IS NULL) AND (approved_by IS NULL) = (approved_digest IS NULL) AND (approved_by IS NULL) = (envelope IS NULL)),
  CONSTRAINT pay_run_version_approved_the_digest CHECK (approved_digest IS NULL OR approved_digest = digest),
  CONSTRAINT pay_run_version_state_approved CHECK (state NOT IN ('approved', 'posted', 'paid_in_part', 'paid', 'closed') OR approved_by IS NOT NULL),
  CONSTRAINT pay_run_version_reason CHECK (coalesce(length(reason), 0) <= 500)
);
CREATE INDEX pay_run_version_run_idx ON nemura.pay_run_version (organisation_id, run_id, version);

CREATE TABLE nemura.pay_run_line (
  organisation_id uuid          NOT NULL DEFAULT nemura.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  version_id      uuid          NOT NULL,
  person_id       uuid          NOT NULL,
  gross           numeric(20,4) NOT NULL,
  ow_subject      numeric(20,4) NOT NULL,
  aw_subject      numeric(20,4) NOT NULL,
  aw_paid         numeric(20,4) NOT NULL,
  cpf_total       numeric(20,4) NOT NULL,
  cpf_employee    numeric(20,4) NOT NULL,
  cpf_employer    numeric(20,4) NOT NULL,
  sdl             numeric(20,4) NOT NULL,
  shg             numeric(20,4) NOT NULL,
  deductions      numeric(20,4) NOT NULL,
  reimbursements  numeric(20,4) NOT NULL,
  held            numeric(20,4) NOT NULL,
  net             numeric(20,4) NOT NULL,
  disburse        numeric(20,4) NOT NULL,
  employer_cost   numeric(20,4) NOT NULL,
  items           jsonb         NOT NULL,   -- the pay items with their class and the input each came from
  working         jsonb         NOT NULL,   -- each step of the calculation with its rule
  flags           jsonb         NOT NULL,
  computed        jsonb         NOT NULL,   -- the figures the engine worked out, where an override replaced one
  CONSTRAINT pay_run_line_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pay_run_line_version_fkey FOREIGN KEY (organisation_id, version_id) REFERENCES nemura.pay_run_version (organisation_id, id),
  CONSTRAINT pay_run_line_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT pay_run_line_once UNIQUE (organisation_id, version_id, person_id)
);
CREATE INDEX pay_run_line_person_idx ON nemura.pay_run_line (organisation_id, person_id);

-- An override replaces one calculated figure of one person's month, with a reason; it is flagged on the approval and is part of the version's digest.
CREATE TABLE nemura.pay_run_override (
  organisation_id uuid          NOT NULL DEFAULT nemura.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  run_id          uuid          NOT NULL,
  person_id       uuid          NOT NULL,
  field           text          NOT NULL,
  value           numeric(20,4) NOT NULL,
  reason          text          NOT NULL,
  created_by      uuid          NOT NULL,
  created_at      timestamptz   NOT NULL,
  withdrawn_at    timestamptz,
  CONSTRAINT pay_run_override_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pay_run_override_run_fkey FOREIGN KEY (organisation_id, run_id) REFERENCES nemura.pay_run (organisation_id, id),
  CONSTRAINT pay_run_override_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT pay_run_override_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT pay_run_override_field CHECK (field IN ('cpf_employee', 'cpf_employer', 'sdl', 'shg')),
  CONSTRAINT pay_run_override_value CHECK (value >= 0 AND value = round(value, 2)),
  CONSTRAINT pay_run_override_reason CHECK (length(btrim(reason)) BETWEEN 10 AND 500)
);
CREATE INDEX pay_run_override_run_idx ON nemura.pay_run_override (organisation_id, run_id) WHERE withdrawn_at IS NULL;

-- A version keeps its figures and its digest; it only moves forward: calculated to approved or superseded, approved to superseded and on (posted and paid states come with the
-- payment path); the approval is written once.
-- +goose StatementBegin
CREATE FUNCTION nemura.pay_run_version_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.run_id IS DISTINCT FROM OLD.run_id OR NEW.version IS DISTINCT FROM OLD.version OR NEW.prepared_by IS DISTINCT FROM OLD.prepared_by OR NEW.prepared_at IS DISTINCT FROM OLD.prepared_at
     OR NEW.supersedes IS DISTINCT FROM OLD.supersedes OR NEW.reason IS DISTINCT FROM OLD.reason OR NEW.digest IS DISTINCT FROM OLD.digest OR NEW.pack_versions IS DISTINCT FROM OLD.pack_versions
     OR NEW.gross IS DISTINCT FROM OLD.gross OR NEW.net IS DISTINCT FROM OLD.net OR NEW.cpf_employee IS DISTINCT FROM OLD.cpf_employee OR NEW.cpf_employer IS DISTINCT FROM OLD.cpf_employer
     OR NEW.sdl_payable IS DISTINCT FROM OLD.sdl_payable OR NEW.shg IS DISTINCT FROM OLD.shg OR NEW.deductions IS DISTINCT FROM OLD.deductions OR NEW.disburse IS DISTINCT FROM OLD.disburse
     OR NEW.reimbursements IS DISTINCT FROM OLD.reimbursements OR NEW.employer_cost IS DISTINCT FROM OLD.employer_cost OR NEW.people IS DISTINCT FROM OLD.people
     OR (OLD.approved_by IS NOT NULL AND (NEW.approved_by IS DISTINCT FROM OLD.approved_by OR NEW.approved_at IS DISTINCT FROM OLD.approved_at OR NEW.approved_digest IS DISTINCT FROM OLD.approved_digest OR NEW.envelope IS DISTINCT FROM OLD.envelope)) THEN
    RAISE EXCEPTION 'A pay run version keeps its figures and its digest: a change is the next version' USING ERRCODE = 'NM173';
  END IF;
  IF NEW.state IS DISTINCT FROM OLD.state AND NOT ((OLD.state = 'calculated' AND NEW.state IN ('approved', 'superseded')) OR (OLD.state = 'approved' AND NEW.state IN ('superseded', 'posted'))
     OR (OLD.state = 'posted' AND NEW.state IN ('paid_in_part', 'paid', 'superseded')) OR (OLD.state = 'paid_in_part' AND NEW.state IN ('paid')) OR (OLD.state = 'paid' AND NEW.state = 'closed')) THEN
    RAISE EXCEPTION 'A % pay run version cannot become %', OLD.state, NEW.state USING ERRCODE = 'NM173';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER pay_run_version_guard BEFORE UPDATE OR DELETE ON nemura.pay_run_version FOR EACH ROW EXECUTE FUNCTION nemura.pay_run_version_guard();
CREATE TRIGGER pay_run_line_append_only BEFORE UPDATE OR DELETE ON nemura.pay_run_line FOR EACH ROW EXECUTE FUNCTION nemura.append_only_guard('A pay run line');

-- +goose StatementBegin
CREATE FUNCTION nemura.pay_run_override_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.run_id IS DISTINCT FROM OLD.run_id OR NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.field IS DISTINCT FROM OLD.field OR NEW.value IS DISTINCT FROM OLD.value
     OR NEW.reason IS DISTINCT FROM OLD.reason OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at IS DISTINCT FROM OLD.created_at OR OLD.withdrawn_at IS NOT NULL THEN
    RAISE EXCEPTION 'An override keeps its figure and its reason, and is withdrawn once' USING ERRCODE = 'NM174';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER pay_run_override_guard BEFORE UPDATE OR DELETE ON nemura.pay_run_override FOR EACH ROW EXECUTE FUNCTION nemura.pay_run_override_guard();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.pay_run DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('pay_run', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.pay_run_version DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('pay_run_version', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.pay_run_override DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('pay_run_override', 'id');

ALTER TABLE nemura.pay_run ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.pay_run FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.pay_run USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.pay_run_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.pay_run_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.pay_run_version USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.pay_run_line ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.pay_run_line FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.pay_run_line USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.pay_run_override ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.pay_run_override FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.pay_run_override USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.pay_run, nemura.pay_run_version, nemura.pay_run_line, nemura.pay_run_override TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, month, pay_group_id, created_by, created_at) ON nemura.pay_run TO nemura_api;
GRANT INSERT (id, run_id, version, state, prepared_by, prepared_at, supersedes, reason, digest, pack_versions, people, flags_failing, flags_warning, gross, cpf_employee, cpf_employer, sdl_payable, shg,
              deductions, reimbursements, net, disburse, employer_cost), UPDATE (state, approved_by, approved_at, approved_digest, envelope) ON nemura.pay_run_version TO nemura_api;
GRANT INSERT (id, version_id, person_id, gross, ow_subject, aw_subject, aw_paid, cpf_total, cpf_employee, cpf_employer, sdl, shg, deductions, reimbursements, held, net, disburse, employer_cost, items, working, flags, computed) ON nemura.pay_run_line TO nemura_api;
GRANT INSERT (id, run_id, person_id, field, value, reason, created_by, created_at), UPDATE (withdrawn_at) ON nemura.pay_run_override TO nemura_api;
-- The inputs a run uses are marked used by the run's approval, once.
GRANT UPDATE (used_in_run, used_at) ON nemura.pay_one_off TO nemura_api;
GRANT UPDATE (used_in_run, used_at) ON nemura.overtime_entry TO nemura_api;
GRANT UPDATE (used_in_run, used_at) ON nemura.pay_input TO nemura_api;
