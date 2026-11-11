-- name: ListLedgerBoundaries :many
-- The ledger's purge boundaries, oldest first (AUD-10).
SELECT id, boundary, approval, counts, purged_hash, anchor_key, written_at, carried_revision
FROM fyniq.ledger_boundary
ORDER BY boundary;

-- name: InsertLedgerBoundary :one
-- A ledger boundary, in the purge's own transaction: its guard checks the switch, the year and its periods (000292).
INSERT INTO fyniq.ledger_boundary (boundary, approval, counts, purged_hash, anchor_key, written_at)
VALUES (sqlc.arg(boundary), sqlc.arg(approval), sqlc.arg(counts), sqlc.arg(purged_hash), sqlc.arg(anchor_key), sqlc.arg(written_at))
RETURNING id, boundary, approval, counts, purged_hash, anchor_key, written_at, carried_revision;

-- name: EarliestEntryFrom :one
-- The posting date of the earliest entry dated on or after a day; no row when there is none.
SELECT posting_date FROM fyniq.journal_entry WHERE posting_date >= sqlc.arg(day)::date ORDER BY posting_date LIMIT 1;

-- name: LedgerPurgeLines :many
-- The lines of every entry dated before a boundary, with what the purge's rule reads of their entry: its source, what it
-- reverses, and whether a permanent register or the bank's reconciliation names it (share issues, the equity register,
-- share capital, the asset register's depreciation and disposals, bank matches and transfers), as fyniq.ledger_purge
-- refuses to delete those (000292).
SELECT e.id AS entry_id, e.posting_date, e.source_type, e.source_id, e.reverses_entry_id,
       (EXISTS (SELECT 1 FROM fyniq.share_issue s WHERE s.organisation_id = e.organisation_id AND s.entry_id = e.id)
        OR EXISTS (SELECT 1 FROM fyniq.bank_match m WHERE m.organisation_id = e.organisation_id AND m.entry_id = e.id)
        OR EXISTS (SELECT 1 FROM fyniq.bank_transfer t WHERE t.organisation_id = e.organisation_id AND t.entry_id = e.id)
        OR e.source_type IN ('equity_transaction', 'asset_depreciation', 'asset_disposal')
        OR EXISTS (SELECT 1 FROM fyniq.journal_line x JOIN fyniq.account a ON a.organisation_id = x.organisation_id AND a.id = x.account_id
                    WHERE x.organisation_id = e.organisation_id AND x.entry_id = e.id AND a.role = 'share-capital'))::boolean AS named,
       l.account_id, l.currency, l.amount::text AS amount, l.base_amount::text AS base_amount, l.party_id, l.instrument_id,
       coalesce(d.value_ids, '{}')::uuid[] AS dimension_value_ids
FROM fyniq.journal_entry e
JOIN fyniq.journal_line l ON l.organisation_id = e.organisation_id AND l.entry_id = e.id
LEFT JOIN fyniq.dimension_set d ON d.organisation_id = l.organisation_id AND d.id = l.dimension_set_id
WHERE e.posting_date < sqlc.arg(boundary)::date
ORDER BY e.posting_date, e.ledger_revision, l.line_no;

-- name: PurgeLedger :one
-- The purge of the entries given, behind the boundary this transaction recorded (fyniq.ledger_purge, 000292).
SELECT fyniq.ledger_purge(sqlc.arg(boundary)::date, sqlc.arg(entries)::uuid[])::bigint AS purged;

-- name: ReversalLinksAcross :many
-- The entries dated on or after a boundary that a reversal links to one dated before it, either way: the purge keeps
-- both ends (AUD-10).
SELECT e.id, e.posting_date, e.reverses_entry_id
FROM fyniq.journal_entry e
WHERE e.posting_date >= sqlc.arg(boundary)::date
  AND (EXISTS (SELECT 1 FROM fyniq.journal_entry o WHERE o.organisation_id = e.organisation_id AND o.id = e.reverses_entry_id
                AND o.posting_date < sqlc.arg(boundary)::date)
       OR EXISTS (SELECT 1 FROM fyniq.journal_entry r WHERE r.organisation_id = e.organisation_id AND r.reverses_entry_id = e.id
                   AND r.posting_date < sqlc.arg(boundary)::date))
ORDER BY e.posting_date, e.ledger_revision;

-- name: PurgedEntryByIdentity :one
-- A purged posting's tombstone, by its posting identity (AUD-10).
SELECT id, posting_identity, source_type, source_id, posting_date, ledger_revision, reverses_entry_id, boundary
FROM fyniq.ledger_purged_entry WHERE posting_identity = $1;

-- name: PurgedOfSource :many
-- The tombstones of a source document's purged postings, in the order they were posted.
SELECT id, posting_identity, source_type, source_id, posting_date, ledger_revision, reverses_entry_id, boundary
FROM fyniq.ledger_purged_entry WHERE source_id = sqlc.arg(source_id)::text
ORDER BY ledger_revision;

-- name: CarriedEntryOfSource :one
-- A source document's latest carry-forward: the balance its purged postings left (AUD-10).
SELECT id FROM fyniq.journal_entry
WHERE source_type = sqlc.arg(source_type) AND source_id = sqlc.arg(source_id) AND rule_version = 'purge-carry-forward@1'
ORDER BY posting_date DESC, ledger_revision DESC
LIMIT 1;
