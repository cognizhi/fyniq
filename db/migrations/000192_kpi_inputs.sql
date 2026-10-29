-- The counts a person enters for the KPIs until their data exists (PRD-001 FR-KPI-02, rev 0.161; p1-dashboard-kpis
-- task 2.1, design D3).
--
-- A month's paying customers, new customers and customers lost, and its headcount: whole numbers from zero, the three
-- customer counts together or none of them. They are not ledger figures, so a closed month's counts are corrected like
-- any other month's; each change is audited on the row in the same transaction (AUD-01), a change is the row's next
-- version, and a row is never deleted (FY244), so the KPIs a board read trace to the counts behind them.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.kpi_input (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  month            date        NOT NULL,
  paying_customers bigint,
  new_customers    bigint,
  lost_customers   bigint,
  headcount        bigint,
  entered_by       text        NOT NULL,
  entered_at       timestamptz NOT NULL,
  version          integer     NOT NULL DEFAULT 1,
  CONSTRAINT kpi_input_pkey PRIMARY KEY (organisation_id, month),
  CONSTRAINT kpi_input_id_key UNIQUE (organisation_id, id),
  CONSTRAINT kpi_input_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT kpi_input_first_of_month CHECK (month = date_trunc('month', month)::date),
  CONSTRAINT kpi_input_customers_together CHECK ((paying_customers IS NULL) = (new_customers IS NULL)
    AND (new_customers IS NULL) = (lost_customers IS NULL)),
  CONSTRAINT kpi_input_something CHECK (paying_customers IS NOT NULL OR headcount IS NOT NULL),
  CONSTRAINT kpi_input_whole CHECK (paying_customers BETWEEN 0 AND 10000000 AND new_customers BETWEEN 0 AND 10000000
    AND lost_customers BETWEEN 0 AND 10000000 AND headcount BETWEEN 0 AND 1000000),
  CONSTRAINT kpi_input_entered_by CHECK (length(btrim(entered_by)) BETWEEN 1 AND 200),
  CONSTRAINT kpi_input_version CHECK (version >= 1)
);

-- +goose StatementBegin
-- A month is written at version 1 and changed only as its next version, by someone, at a time; it is never deleted
-- (FY244).
CREATE FUNCTION fyniq.kpi_input_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.version <> 1 THEN
      RAISE EXCEPTION 'A month''s KPI counts are written at version 1'
        USING ERRCODE = 'FY244', HINT = 'Enter the month''s counts; a change is its next version (FR-KPI-02).';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP <> 'UPDATE' THEN
    RAISE EXCEPTION 'KPI counts are never deleted or truncated'
      USING ERRCODE = 'FY244', HINT = 'Enter the corrected counts as the month''s next version (FR-KPI-02).';
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.month IS DISTINCT FROM OLD.month OR NEW.version <> OLD.version + 1
     OR NEW.entered_at < OLD.entered_at THEN
    RAISE EXCEPTION 'The counts of % change only as their next version, entered later', OLD.month
      USING ERRCODE = 'FY244', HINT = 'Read the month''s counts and enter the change as their next version (FR-KPI-02).';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER kpi_input_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.kpi_input
  FOR EACH ROW EXECUTE FUNCTION fyniq.kpi_input_guard();
CREATE TRIGGER kpi_input_no_truncate BEFORE TRUNCATE ON fyniq.kpi_input
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.kpi_input_guard();

ALTER TABLE fyniq.kpi_input ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.kpi_input FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.kpi_input
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Entered and changed: each audited on the month in the same transaction (AUD-01).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.kpi_input
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('kpi_input', 'id');

REVOKE ALL ON fyniq.kpi_input FROM PUBLIC;
GRANT SELECT ON fyniq.kpi_input TO fyniq_api, fyniq_readonly;
GRANT INSERT (month, paying_customers, new_customers, lost_customers, headcount, entered_by, entered_at) ON fyniq.kpi_input TO fyniq_api;
GRANT UPDATE (paying_customers, new_customers, lost_customers, headcount, entered_by, entered_at, version) ON fyniq.kpi_input TO fyniq_api;

