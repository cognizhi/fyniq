-- name: BalancesForPeriod :many
SELECT account_id, period_id, currency, amount, base_amount, line_count
FROM fyniq.balance WHERE period_id = $1 ORDER BY account_id, currency;

-- name: BalanceDifferences :many
SELECT d.account_id::uuid AS account_id, d.period_id::uuid AS period_id, d.currency::text AS currency,
       -- '' where one side has no row
       coalesce(d.cached_amount::text, '')::text AS cached_amount,
       coalesce(d.cached_base_amount::text, '')::text AS cached_base_amount,
       coalesce(d.lines_amount::text, '')::text AS lines_amount,
       coalesce(d.lines_base_amount::text, '')::text AS lines_base_amount
FROM fyniq.balance_differences() AS d(account_id, period_id, currency, cached_amount, cached_base_amount,
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
