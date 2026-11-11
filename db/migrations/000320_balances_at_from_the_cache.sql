-- A period's balances as at a revision read the cache when nothing later is in the period (PRD-001 NFR-PERF-02, FR-RPT-08,
-- INV-08; p1-operations-dr task 13.5, design D11, rev 0.250).
--
-- fyniq.balances_at summed every line of a period's entries up to the revision, so a twelve-month report with its
-- comparative read every line of 24 periods, and its time grew with the ledger. When no entry of the period is later
-- than the revision, those lines are every line of the period, and the balances cache holds their sums (INV-08: the same
-- transaction keeps it, and the nightly verification compares it with the lines): the function reads the cache then,
-- its dimension sets summed, as BalancesForPeriod does. A row with no line is left out, as the lines' sum has none. A
-- report at an earlier revision, with later entries in the period, still sums the lines up to it. Whether a later entry
-- exists is one probe of the entries' unique (organisation_id, ledger_revision) index. The checks keep reading the lines
-- (balance_lines_at), so a statement they check never takes its figures from the cache it is compared with.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
-- balance_lines_at is what balances_at always was: the lines of the period's entries up to the revision, summed. The
-- checks read it (storage.WithLines), so the statements they check stay apart from the cache they compare with.
CREATE FUNCTION fyniq.balance_lines_at(p_period uuid, p_revision bigint)
RETURNS TABLE (account_id uuid, period_id uuid, currency text, amount numeric, base_amount numeric, line_count bigint)
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT l.account_id, e.period_id, l.currency, sum(l.amount), sum(l.base_amount), count(*)
    FROM fyniq.journal_line l
    JOIN fyniq.journal_entry e ON e.organisation_id = l.organisation_id AND e.id = l.entry_id
   WHERE l.organisation_id = fyniq.current_org() AND e.period_id = p_period AND e.ledger_revision <= p_revision
   GROUP BY l.account_id, e.period_id, l.currency
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.balances_at(p_period uuid, p_revision bigint)
RETURNS TABLE (account_id uuid, period_id uuid, currency text, amount numeric, base_amount numeric, line_count bigint)
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  WITH later AS (
    SELECT EXISTS (SELECT 1 FROM fyniq.journal_entry e
                    WHERE e.organisation_id = fyniq.current_org() AND e.ledger_revision > p_revision AND e.period_id = p_period) AS any
  )
  SELECT b.account_id, b.period_id, b.currency, sum(b.amount), sum(b.base_amount), sum(b.line_count)::bigint
    FROM fyniq.balance b, later
   WHERE NOT later.any AND b.organisation_id = fyniq.current_org() AND b.period_id = p_period
   GROUP BY b.account_id, b.period_id, b.currency
  HAVING sum(b.line_count) > 0
  UNION ALL
  SELECT x.* FROM later, fyniq.balance_lines_at(p_period, p_revision) x WHERE later.any
$$;
-- +goose StatementEnd

GRANT EXECUTE ON FUNCTION fyniq.balance_lines_at(uuid, bigint) TO fyniq_api, fyniq_worker, fyniq_readonly;
