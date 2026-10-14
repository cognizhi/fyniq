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
