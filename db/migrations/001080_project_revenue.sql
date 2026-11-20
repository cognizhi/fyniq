-- Revenue over time for a fixed-fee project: the accountant's election and each month's proposed journal (PRD-001 FR-PRJ-11, rev 0.572;
-- p2-customer-portal-subscriptions task 4.1, second half).
--
-- A project is elected for revenue over time by cost-to-cost progress with an estimate of its total cost; the election is a record that changes by version
-- (audited). Each month's journal is proposed once: the run records the draft journal it made, the month and the adjustment, and never changes. The figures come
-- from the ledger and the milestones at proposal time (internal/domain/projectrevenue); nothing here posts.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.project_revenue_election (
  organisation_id uuid          NOT NULL DEFAULT fyniq.current_org(),
  project_id      uuid          NOT NULL,
  method          text          NOT NULL,
  estimated_cost  numeric(20,4),          -- the estimated total cost, in the project's currency; set for over_time
  version         integer       NOT NULL DEFAULT 1,
  set_by          text          NOT NULL,
  set_at          timestamptz   NOT NULL,
  CONSTRAINT project_revenue_election_pkey PRIMARY KEY (organisation_id, project_id),
  CONSTRAINT project_revenue_election_project_fkey FOREIGN KEY (organisation_id, project_id) REFERENCES fyniq.project (organisation_id, id),
  CONSTRAINT project_revenue_election_method_known CHECK (method IN ('milestone', 'over_time')),
  CONSTRAINT project_revenue_election_estimate_form CHECK ((method = 'over_time') = (estimated_cost IS NOT NULL) AND (estimated_cost IS NULL OR (estimated_cost <> 'NaN' AND estimated_cost > 0))),
  CONSTRAINT project_revenue_election_by_form CHECK (set_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

CREATE TABLE fyniq.project_revenue_run (
  organisation_id uuid          NOT NULL DEFAULT fyniq.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  project_id      uuid          NOT NULL,
  month           date          NOT NULL,   -- the first day of the month
  journal_id      uuid          NOT NULL,   -- the draft journal proposed for approval
  adjustment      numeric(20,4) NOT NULL,   -- the revenue it books (negative when invoiced ahead of progress)
  earned          numeric(20,4) NOT NULL,   -- the revenue earned to date after it
  created_by      text          NOT NULL,
  created_at      timestamptz   NOT NULL,
  CONSTRAINT project_revenue_run_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT project_revenue_run_project_fkey FOREIGN KEY (organisation_id, project_id) REFERENCES fyniq.project (organisation_id, id),
  CONSTRAINT project_revenue_run_journal_fkey FOREIGN KEY (organisation_id, journal_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT project_revenue_run_month_first CHECK (month = date_trunc('month', month)::date),
  CONSTRAINT project_revenue_run_amounts CHECK (adjustment <> 'NaN' AND earned <> 'NaN' AND earned >= 0),
  CONSTRAINT project_revenue_run_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
-- A month is proposed once per project.
CREATE UNIQUE INDEX project_revenue_run_month ON fyniq.project_revenue_run (organisation_id, project_id, month);

-- +goose StatementBegin
CREATE FUNCTION fyniq.project_revenue_run_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A month''s revenue run is never changed or deleted' USING ERRCODE = 'FYP23',
    HINT = 'Reverse the journal if it was wrong; a month is proposed once (FR-PRJ-11).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER project_revenue_run_guard BEFORE UPDATE OR DELETE ON fyniq.project_revenue_run FOR EACH ROW EXECUTE FUNCTION fyniq.project_revenue_run_guard();
CREATE TRIGGER project_revenue_run_no_truncate BEFORE TRUNCATE ON fyniq.project_revenue_run FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER project_revenue_election_no_truncate BEFORE TRUNCATE ON fyniq.project_revenue_election FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.project_revenue_election ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.project_revenue_election FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.project_revenue_election USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.project_revenue_election
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('project_revenue_election', 'project_id');
ALTER TABLE fyniq.project_revenue_run ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.project_revenue_run FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.project_revenue_run USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.project_revenue_run
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('project_revenue_run', 'id');

GRANT SELECT ON fyniq.project_revenue_election, fyniq.project_revenue_run TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (project_id, method, estimated_cost, set_by, set_at) ON fyniq.project_revenue_election TO fyniq_api;
GRANT UPDATE (method, estimated_cost, version, set_by, set_at) ON fyniq.project_revenue_election TO fyniq_api;
GRANT INSERT (project_id, month, journal_id, adjustment, earned, created_by, created_at) ON fyniq.project_revenue_run TO fyniq_api;
