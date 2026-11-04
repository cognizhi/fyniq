-- The balances cache by dimension set (PRD-001 FR-DIM-02, §11.4, INV-08; p1-organisation-chart task 3.3, slice 1): the
-- dimension-set keys 000218 added are validated, the old key goes (000219 built the new one), and the posting trigger,
-- the rebuild and the comparison of the cache with the lines group by the set, NULL being the set of no dimension.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.journal_line VALIDATE CONSTRAINT journal_line_dimension_set_fkey;
ALTER TABLE fyniq.balance VALIDATE CONSTRAINT balance_dimension_set_fkey;
ALTER TABLE fyniq.balance DROP CONSTRAINT balance_key;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.journal_line_balances() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  INSERT INTO fyniq.balance AS b
         (organisation_id, account_id, period_id, dimension_set_id, currency, minor_units, amount, base_amount, line_count)
  SELECT l.organisation_id, l.account_id, e.period_id, l.dimension_set_id, l.currency, l.minor_units,
         sum(l.amount), sum(l.base_amount), count(*)
    FROM new_lines l
    JOIN fyniq.journal_entry e ON e.organisation_id = l.organisation_id AND e.id = l.entry_id
   GROUP BY l.organisation_id, l.account_id, e.period_id, l.dimension_set_id, l.currency, l.minor_units
  ON CONFLICT (organisation_id, account_id, period_id, dimension_set_id, currency) DO UPDATE
     SET amount = b.amount + excluded.amount,
         base_amount = b.base_amount + excluded.base_amount,
         line_count = b.line_count + excluded.line_count;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

-- The cache against a recomputation from the lines, per set: no rows means they agree (INV-08).
DROP FUNCTION fyniq.balance_differences();
-- +goose StatementBegin
CREATE FUNCTION fyniq.balance_differences()
RETURNS TABLE (account_id uuid, period_id uuid, dimension_set_id uuid, currency text,
               cached_amount numeric, cached_base_amount numeric,
               lines_amount numeric, lines_base_amount numeric)
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  WITH lines AS (
    SELECT l.account_id, e.period_id, l.dimension_set_id, l.currency,
           sum(l.amount) AS amount, sum(l.base_amount) AS base_amount, count(*) AS line_count
      FROM fyniq.journal_line l
      JOIN fyniq.journal_entry e ON e.organisation_id = l.organisation_id AND e.id = l.entry_id
     WHERE l.organisation_id = fyniq.current_org()
     GROUP BY l.account_id, e.period_id, l.dimension_set_id, l.currency)
  SELECT coalesce(b.account_id, l.account_id), coalesce(b.period_id, l.period_id),
         CASE WHEN b.account_id IS NULL THEN l.dimension_set_id ELSE b.dimension_set_id END,
         coalesce(b.currency, l.currency), b.amount, b.base_amount, l.amount, l.base_amount
    FROM (SELECT * FROM fyniq.balance WHERE organisation_id = fyniq.current_org()) b
    FULL JOIN lines l
      ON l.account_id = b.account_id AND l.period_id = b.period_id AND l.currency = b.currency
     AND l.dimension_set_id IS NOT DISTINCT FROM b.dimension_set_id
   WHERE b.amount IS DISTINCT FROM l.amount
      OR b.base_amount IS DISTINCT FROM l.base_amount
      OR b.line_count IS DISTINCT FROM l.line_count
$$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION fyniq.balance_differences() TO fyniq_api, fyniq_worker, fyniq_readonly;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.rebuild_balances() RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
  n   bigint;
BEGIN
  PERFORM fyniq.lock_ledger();
  DELETE FROM fyniq.balance WHERE organisation_id = org;
  INSERT INTO fyniq.balance
         (organisation_id, account_id, period_id, dimension_set_id, currency, minor_units, amount, base_amount, line_count)
  SELECT l.organisation_id, l.account_id, e.period_id, l.dimension_set_id, l.currency, l.minor_units,
         sum(l.amount), sum(l.base_amount), count(*)
    FROM fyniq.journal_line l
    JOIN fyniq.journal_entry e ON e.organisation_id = l.organisation_id AND e.id = l.entry_id
   WHERE l.organisation_id = org
   GROUP BY l.organisation_id, l.account_id, e.period_id, l.dimension_set_id, l.currency, l.minor_units;
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n;
END
$$;
-- +goose StatementEnd

