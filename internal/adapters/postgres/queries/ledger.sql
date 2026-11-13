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
  (entry_id, line_no, account_id, currency, amount, fx_rate, fx_rate_source, base_amount, description, party_id, dimension_set_id,
   instrument_id)
SELECT sqlc.arg(entry_id)::uuid,
       unnest(sqlc.arg(line_nos)::int[]),
       unnest(sqlc.arg(account_ids)::uuid[]),
       unnest(sqlc.arg(currencies)::text[]),
       unnest(sqlc.arg(amounts)::numeric[]),
       unnest(sqlc.arg(fx_rates)::numeric[]),
       unnest(sqlc.arg(fx_rate_sources)::text[]),
       unnest(sqlc.arg(base_amounts)::numeric[]),
       nullif(unnest(sqlc.arg(descriptions)::text[]), ''),
       nullif(unnest(sqlc.arg(party_ids)::text[]), '')::uuid,
       nullif(unnest(sqlc.arg(dimension_set_ids)::text[]), '')::uuid,
       nullif(unnest(sqlc.arg(instrument_ids)::text[]), '')::uuid;

-- name: GetEntry :one
SELECT id, ledger_revision, posting_date, period_id, posting_identity, source_type, source_id,
       source_version, rule_version, reverses_entry_id, memo, recorded_at
FROM fyniq.journal_entry WHERE id = $1;

-- name: GetEntryLines :many
-- A line's dimension values are its set's, in ascending order; none for a line with no set (FR-DIM-02).
SELECT l.id, l.line_no, l.account_id, l.currency, l.amount, l.fx_rate, l.fx_rate_source, l.base_currency, l.base_amount,
       l.description, l.party_id, coalesce(d.value_ids, '{}')::uuid[] AS dimension_value_ids, l.instrument_id
FROM fyniq.journal_line l
LEFT JOIN fyniq.dimension_set d ON d.organisation_id = l.organisation_id AND d.id = l.dimension_set_id
WHERE l.entry_id = $1 ORDER BY l.line_no;

-- name: DimensionSetOf :one
-- The set that holds a combination of values, written in its canonical form (§11.4).
SELECT id FROM fyniq.dimension_set WHERE value_ids = sqlc.arg(value_ids)::uuid[];

-- name: InsertDimensionSet :one
-- A combination's set, written once: none comes back when another transaction wrote it first.
INSERT INTO fyniq.dimension_set (value_ids) VALUES (sqlc.arg(value_ids)::uuid[])
ON CONFLICT (organisation_id, value_ids) DO NOTHING
RETURNING id;

-- name: GetLedgerRevision :one
SELECT revision FROM fyniq.ledger_revision;

-- name: GetEntryIDByIdentity :one
SELECT id FROM fyniq.journal_entry WHERE posting_identity = $1;

-- name: AccountLines :many
-- An account's lines dated from..to, in posting order: date, then revision, then line (FR-GL-11), with their entry's
-- provenance and the source document's number when it has one.
SELECT e.id AS entry_id, e.ledger_revision, e.posting_date, e.source_type, e.source_id, e.source_version, e.memo,
       e.reverses_entry_id, l.line_no, l.currency, l.amount, l.base_amount, l.description, n.number, l.party_id,
       coalesce(d.value_ids, '{}')::uuid[] AS dimension_value_ids, l.instrument_id
FROM fyniq.journal_line l
JOIN fyniq.journal_entry e ON e.organisation_id = l.organisation_id AND e.id = l.entry_id
LEFT JOIN fyniq.document_number n ON n.organisation_id = e.organisation_id AND n.id::text = e.source_id
LEFT JOIN fyniq.dimension_set d ON d.organisation_id = l.organisation_id AND d.id = l.dimension_set_id
WHERE l.account_id = sqlc.arg(account_id) AND e.posting_date BETWEEN sqlc.arg(date_from) AND sqlc.arg(date_to)
ORDER BY e.posting_date, e.ledger_revision, l.line_no
LIMIT sqlc.arg(row_limit);

-- name: AccountLinesWithAmount :many
-- AccountLines in a currency of exactly an amount (NFR-PERF-04): what a bank line may be the money of, on the account's
-- index by the line's own date.
SELECT e.id AS entry_id, e.ledger_revision, e.posting_date, e.source_type, e.source_id, e.source_version, e.memo,
       e.reverses_entry_id, l.line_no, l.currency, l.amount, l.base_amount, l.description, n.number, l.party_id,
       coalesce(d.value_ids, '{}')::uuid[] AS dimension_value_ids, l.instrument_id
FROM fyniq.journal_line l
JOIN fyniq.journal_entry e ON e.organisation_id = l.organisation_id AND e.id = l.entry_id
LEFT JOIN fyniq.document_number n ON n.organisation_id = e.organisation_id AND n.id::text = e.source_id
LEFT JOIN fyniq.dimension_set d ON d.organisation_id = l.organisation_id AND d.id = l.dimension_set_id
WHERE l.account_id = sqlc.arg(account_id) AND l.posting_date BETWEEN sqlc.arg(date_from) AND sqlc.arg(date_to)
  AND l.currency = sqlc.arg(currency) AND l.amount = sqlc.arg(amount)::numeric
ORDER BY l.posting_date, e.ledger_revision, l.line_no
LIMIT sqlc.arg(row_limit);

-- name: AccountLinesAfter :many
-- AccountLines a page at a time, after a line's place in posting order (NFR-PERF-05): the line's own posting date, on
-- the account's index, gives them in date order from the page's first day, and an incremental sort orders each day by
-- revision and line, so a page reads about a page of lines however long the ledger is.
SELECT e.id AS entry_id, e.ledger_revision, e.posting_date, e.source_type, e.source_id, e.source_version, e.memo,
       e.reverses_entry_id, l.line_no, l.currency, l.amount, l.base_amount, l.description, n.number, l.party_id,
       coalesce(d.value_ids, '{}')::uuid[] AS dimension_value_ids, l.instrument_id
FROM fyniq.journal_line l
JOIN fyniq.journal_entry e ON e.organisation_id = l.organisation_id AND e.id = l.entry_id
LEFT JOIN fyniq.document_number n ON n.organisation_id = e.organisation_id AND n.id::text = e.source_id
LEFT JOIN fyniq.dimension_set d ON d.organisation_id = l.organisation_id AND d.id = l.dimension_set_id
WHERE l.account_id = sqlc.arg(account_id) AND l.posting_date BETWEEN sqlc.arg(date_from) AND sqlc.arg(date_to)
  AND l.posting_date >= sqlc.arg(after_date)
  AND (l.posting_date, e.ledger_revision, l.line_no) > (sqlc.arg(after_date)::date, sqlc.arg(after_revision)::bigint, sqlc.arg(after_line)::integer)
ORDER BY l.posting_date, e.ledger_revision, l.line_no
LIMIT sqlc.arg(row_limit);

-- name: EntriesAfter :many
-- The entries after a ledger revision, in revision order, a page at a time, without their lines, which LinesOfEntries
-- reads for the whole page at once (NFR-PERF-02): the replay (ENG-06, QA-ENG-06) and the statements' movements walk them.
SELECT id, ledger_revision, posting_date, period_id, posting_identity, source_type, source_id,
       source_version, rule_version, reverses_entry_id, memo, recorded_at
FROM fyniq.journal_entry
WHERE ledger_revision > sqlc.arg(after_revision)
ORDER BY ledger_revision
LIMIT sqlc.arg(page_size);

-- name: EntriesOfSourceType :many
-- Every entry of a source type, in revision order, without their lines (LinesOfEntries reads them), on
-- journal_entry_source_type (NFR-PERF-02).
SELECT id, ledger_revision, posting_date, period_id, posting_identity, source_type, source_id,
       source_version, rule_version, reverses_entry_id, memo, recorded_at
FROM fyniq.journal_entry
WHERE source_type = sqlc.arg(source_type)
ORDER BY ledger_revision;

-- name: LinesOfEntries :many
-- The lines of a page of entries, each entry's in line order: one read for the page, as GetEntryLines reads one entry's.
SELECT l.entry_id, l.id, l.line_no, l.account_id, l.currency, l.amount, l.fx_rate, l.fx_rate_source, l.base_currency,
       l.base_amount, l.description, l.party_id, coalesce(d.value_ids, '{}')::uuid[] AS dimension_value_ids, l.instrument_id
FROM fyniq.journal_line l
LEFT JOIN fyniq.dimension_set d ON d.organisation_id = l.organisation_id AND d.id = l.dimension_set_id
WHERE l.entry_id = ANY(sqlc.arg(entry_ids)::uuid[])
ORDER BY l.entry_id, l.line_no;

-- name: EntryIDsOfSource :many
-- Every entry naming a source document, reversals included, in revision order: an asset's months of depreciation.
SELECT id FROM fyniq.journal_entry
WHERE source_id = sqlc.arg(source_id)::text
ORDER BY ledger_revision;
