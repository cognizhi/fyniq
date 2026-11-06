-- Balances by dimension set as at a ledger revision (PRD-001 FR-DIM-02, FR-RPT-08; p1-organisation-chart task 3.3,
-- slice 4).
--
-- fyniq.balances_by_set_at(period, revision) is fyniq.balances_at with each dimension set apart: a P&L by dimension reads
-- a department's lines as at the revision the statement records, as every statement reads the ledger (FR-RPT-08). Its
-- value_ids are '{}' for the lines with no dimension. Only the balance service calls it (ENG-04).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.balances_by_set_at(p_period uuid, p_revision bigint)
RETURNS TABLE (account_id uuid, period_id uuid, currency text, value_ids uuid[], amount numeric, base_amount numeric, line_count bigint)
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT l.account_id, e.period_id, l.currency, coalesce(s.value_ids, '{}'::uuid[]), sum(l.amount), sum(l.base_amount), count(*)
    FROM fyniq.journal_line l
    JOIN fyniq.journal_entry e ON e.organisation_id = l.organisation_id AND e.id = l.entry_id
    LEFT JOIN fyniq.dimension_set s ON s.organisation_id = l.organisation_id AND s.id = l.dimension_set_id
   WHERE l.organisation_id = fyniq.current_org() AND e.period_id = p_period AND e.ledger_revision <= p_revision
   GROUP BY l.account_id, e.period_id, l.currency, s.value_ids
$$;
-- +goose StatementEnd

GRANT EXECUTE ON FUNCTION fyniq.balances_by_set_at(uuid, bigint) TO fyniq_api, fyniq_worker, fyniq_readonly;
