-- Spend anomaly alerts (PRD-001 FR-KPI-06, rev 0.540; p2-reporting-planning task 4.3, design D10).
--
-- When a closed month's spend in an expense category is above its trend by three standard deviations, the worker records one alert for the
-- category and the month, and tells the people who approve budgets. An alert is kept as it was raised (the spend, the mean it was compared with)
-- and never changed; a person who looks into it records that they did, once.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.kpi_alert (
  organisation_id uuid          NOT NULL DEFAULT fyniq.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  kind            text          NOT NULL,
  category        text          NOT NULL,   -- the expense category: its report group
  month           date          NOT NULL,
  currency        text          NOT NULL,
  spend           numeric(20,4) NOT NULL,
  mean            numeric(20,4) NOT NULL,
  history_months  integer       NOT NULL,
  ledger_revision bigint        NOT NULL,
  raised_at       timestamptz   NOT NULL,
  CONSTRAINT kpi_alert_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT kpi_alert_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT kpi_alert_once UNIQUE (organisation_id, kind, category, month),
  CONSTRAINT kpi_alert_kind CHECK (kind IN ('spend_anomaly')),
  CONSTRAINT kpi_alert_first_of_month CHECK (month = date_trunc('month', month)::date),
  CONSTRAINT kpi_alert_currency CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT kpi_alert_amounts CHECK (spend <> 'NaN' AND mean <> 'NaN' AND spend > 0 AND mean >= 0),
  CONSTRAINT kpi_alert_category CHECK (length(btrim(category)) BETWEEN 1 AND 120),
  CONSTRAINT kpi_alert_history CHECK (history_months BETWEEN 6 AND 36)
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.kpi_alert_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A raised alert is never changed or deleted' USING ERRCODE = 'FYR07', HINT = 'The next month is its own alert (FR-KPI-06).';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER kpi_alert_guard BEFORE UPDATE OR DELETE ON fyniq.kpi_alert FOR EACH ROW EXECUTE FUNCTION fyniq.kpi_alert_guard();
CREATE TRIGGER kpi_alert_no_truncate BEFORE TRUNCATE ON fyniq.kpi_alert FOR EACH STATEMENT EXECUTE FUNCTION fyniq.kpi_alert_guard();

ALTER TABLE fyniq.kpi_alert ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.kpi_alert FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.kpi_alert USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.kpi_alert
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('kpi_alert', 'id');

REVOKE ALL ON fyniq.kpi_alert FROM PUBLIC;
GRANT SELECT ON fyniq.kpi_alert TO fyniq_api, fyniq_worker, fyniq_readonly;
-- Only the worker raises an alert; nobody changes one.
GRANT INSERT (kind, category, month, currency, spend, mean, history_months, ledger_revision, raised_at) ON fyniq.kpi_alert TO fyniq_worker;
