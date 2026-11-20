-- The weekly cash projection snapshots behind the forecast accuracy view (PRD-001 FR-PLN-04; p2-reporting-planning task 6.1, design D12).
--
-- Each week the worker keeps the short-term projection (FR-PLN-09) as it was that week: the money accounts' balance, the 13 weeks and every expected movement.
-- Four and thirteen weeks later it is compared with the cash that came. A snapshot is kept as it was taken and never changed or deleted: the figures are in
-- one jsonb document as decimal strings, so no number is stored as a float. One snapshot a week (the Monday of the organisation's week).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.cash_snapshot (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  week_of         date        NOT NULL,   -- the Monday of the week it stands for
  taken_on        date        NOT NULL,   -- the organisation's day it was taken: its first day is the projection's today
  currency        text        NOT NULL,
  ledger_revision bigint      NOT NULL,
  projection      jsonb       NOT NULL,
  taken_at        timestamptz NOT NULL,
  CONSTRAINT cash_snapshot_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT cash_snapshot_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT cash_snapshot_week_once UNIQUE (organisation_id, week_of),
  CONSTRAINT cash_snapshot_monday CHECK (extract(isodow FROM week_of) = 1 AND taken_on >= week_of AND taken_on < week_of + 7),
  CONSTRAINT cash_snapshot_currency CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT cash_snapshot_form CHECK (jsonb_typeof(projection) = 'object' AND jsonb_typeof(projection -> 'weekly') = 'array' AND jsonb_typeof(projection -> 'items') = 'array'
    AND jsonb_typeof(projection -> 'opening') = 'string')
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.cash_snapshot_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A cash snapshot is never changed or deleted' USING ERRCODE = 'FYR11', HINT = 'The next week is its own snapshot (FR-PLN-04).';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER cash_snapshot_guard BEFORE UPDATE OR DELETE ON fyniq.cash_snapshot FOR EACH ROW EXECUTE FUNCTION fyniq.cash_snapshot_guard();
CREATE TRIGGER cash_snapshot_no_truncate BEFORE TRUNCATE ON fyniq.cash_snapshot FOR EACH STATEMENT EXECUTE FUNCTION fyniq.cash_snapshot_guard();

ALTER TABLE fyniq.cash_snapshot ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.cash_snapshot FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.cash_snapshot USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.cash_snapshot
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('cash_snapshot', 'id');

REVOKE ALL ON fyniq.cash_snapshot FROM PUBLIC;
GRANT SELECT ON fyniq.cash_snapshot TO fyniq_api, fyniq_worker, fyniq_readonly;
-- Only the worker takes a snapshot; nobody changes one.
GRANT INSERT (week_of, taken_on, currency, ledger_revision, projection, taken_at) ON fyniq.cash_snapshot TO fyniq_worker;

-- +goose Down
-- squawk-ignore ban-drop-table
DROP TABLE fyniq.cash_snapshot;
DROP FUNCTION fyniq.cash_snapshot_guard();
