-- name: BalancesForPeriod :many
-- A period's balances, the dimension sets of each account and currency summed (FR-DIM-02: a reader asks for a
-- dimension to see it apart).
SELECT account_id, period_id, currency, sum(amount)::numeric(20, 4) AS amount,
       sum(base_amount)::numeric(20, 4) AS base_amount, sum(line_count)::bigint AS line_count
FROM fyniq.balance WHERE period_id = $1 GROUP BY account_id, period_id, currency ORDER BY account_id, currency;

-- name: BalancesForPeriodBySet :many
-- A period's balances, each dimension set on its own row with its values, '{}' for the lines with no dimension
-- (FR-DIM-02). The balance service alone reads it.
SELECT b.account_id, b.period_id, b.currency, coalesce(s.value_ids, '{}')::uuid[] AS value_ids,
       b.amount::numeric(20, 4) AS amount, b.base_amount::numeric(20, 4) AS base_amount, b.line_count
FROM fyniq.balance b
LEFT JOIN fyniq.dimension_set s ON s.organisation_id = b.organisation_id AND s.id = b.dimension_set_id
WHERE b.period_id = $1 ORDER BY b.account_id, b.currency, b.dimension_set_id NULLS FIRST;

-- name: BalanceDifferences :many
SELECT d.account_id::uuid AS account_id, d.period_id::uuid AS period_id, d.currency::text AS currency,
       coalesce(d.dimension_set_id::text, '')::text AS dimension_set_id, -- '' for the lines with no dimension
       -- '' where one side has no row
       coalesce(d.cached_amount::text, '')::text AS cached_amount,
       coalesce(d.cached_base_amount::text, '')::text AS cached_base_amount,
       coalesce(d.lines_amount::text, '')::text AS lines_amount,
       coalesce(d.lines_base_amount::text, '')::text AS lines_base_amount
FROM fyniq.balance_differences() AS d(account_id, period_id, dimension_set_id, currency, cached_amount, cached_base_amount,
                                      lines_amount, lines_base_amount);

-- name: RebuildBalances :one
SELECT fyniq.rebuild_balances()::bigint AS rows;

-- name: BalancesForPeriodAt :many
-- A period's balances as at a ledger revision (FR-RPT-08, ENG-46), from fyniq.balances_at. The balance service alone
-- reads it.
SELECT account_id::uuid AS account_id, period_id::uuid AS period_id, currency::text AS currency,
       amount::numeric(20, 4) AS amount, base_amount::numeric(20, 4) AS base_amount, line_count::bigint AS line_count
FROM fyniq.balances_at(sqlc.arg(period_id)::uuid, sqlc.arg(ledger_revision)::bigint) AS b
ORDER BY account_id, currency;
