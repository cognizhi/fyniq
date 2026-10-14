-- name: InsertEntry :one
-- journal_entry_assign sets the revision, the period and the system columns (ENG-46, INV-03).
INSERT INTO fyniq.journal_entry
  (posting_date, posting_identity, source_type, source_id, source_version, rule_version, reverses_entry_id, memo)
VALUES (sqlc.arg(posting_date), sqlc.arg(posting_identity), sqlc.arg(source_type), sqlc.arg(source_id),
        sqlc.arg(source_version), sqlc.arg(rule_version), sqlc.narg(reverses_entry_id), sqlc.narg(memo))
RETURNING id, ledger_revision, posting_date, period_id, recorded_at;

-- name: InsertLines :exec
-- One statement for all of an entry's lines, so the balances cache updates once (INV-08).
INSERT INTO fyniq.journal_line
  (entry_id, line_no, account_id, currency, amount, fx_rate, fx_rate_source, base_amount, description)
SELECT sqlc.arg(entry_id)::uuid,
       unnest(sqlc.arg(line_nos)::int[]),
       unnest(sqlc.arg(account_ids)::uuid[]),
       unnest(sqlc.arg(currencies)::text[]),
       unnest(sqlc.arg(amounts)::numeric[]),
       unnest(sqlc.arg(fx_rates)::numeric[]),
       unnest(sqlc.arg(fx_rate_sources)::text[]),
       unnest(sqlc.arg(base_amounts)::numeric[]),
       nullif(unnest(sqlc.arg(descriptions)::text[]), '');

-- name: GetEntry :one
SELECT id, ledger_revision, posting_date, period_id, posting_identity, source_type, source_id,
       source_version, rule_version, reverses_entry_id, memo, recorded_at
FROM fyniq.journal_entry WHERE id = $1;

-- name: GetEntryLines :many
SELECT id, line_no, account_id, currency, amount, fx_rate, fx_rate_source, base_currency, base_amount, description
FROM fyniq.journal_line WHERE entry_id = $1 ORDER BY line_no;

-- name: GetLedgerRevision :one
SELECT revision FROM fyniq.ledger_revision;
