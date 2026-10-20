-- name: InsertCloseCheck :one
INSERT INTO fyniq.period_close_check (period_id, checked_by, checked_at, ledger_revision, result, clean)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, period_id, checked_by, checked_at, ledger_revision, result, clean;

-- name: GetCloseCheck :one
SELECT id, period_id, checked_by, checked_at, ledger_revision, result, clean FROM fyniq.period_close_check WHERE id = $1;

-- name: LatestCloseCheck :one
SELECT id, period_id, checked_by, checked_at, ledger_revision, result, clean FROM fyniq.period_close_check
WHERE period_id = $1 ORDER BY checked_at DESC, id DESC LIMIT 1;

-- name: InsertPeriodClose :one
INSERT INTO fyniq.period_close (period_id, check_id, attested_by, attested_at, statement, closed_revision, snapshot, snapshot_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, period_id, check_id, attested_by, attested_at, statement, closed_revision, snapshot, snapshot_hash;

-- name: LatestPeriodClose :one
SELECT id, period_id, check_id, attested_by, attested_at, statement, closed_revision, snapshot, snapshot_hash FROM fyniq.period_close
WHERE period_id = $1 ORDER BY attested_at DESC, id DESC LIMIT 1;

-- name: EntriesChangedSince :one
-- Whether an entry dated on or before a day has a revision later than one (FR-GL-07: checks are stale).
SELECT EXISTS (SELECT 1 FROM fyniq.journal_entry WHERE posting_date <= sqlc.arg(through) AND ledger_revision > sqlc.arg(revision))::boolean AS changed;
