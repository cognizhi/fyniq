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

-- name: InsertPeriodReopen :one
INSERT INTO fyniq.period_reopen (period_id, close_id, approval_id, reason, reopened_by, reopened_at, reopened_revision, before_hash,
                                 after_snapshot, after_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, reopened_by, reopened_at;

-- name: LatestPeriodReopen :one
SELECT id, period_id, close_id, approval_id, reason, reopened_by, reopened_at, reopened_revision, before_hash, after_hash
FROM fyniq.period_reopen WHERE period_id = $1 ORDER BY reopened_at DESC, id DESC LIMIT 1;

-- name: InsertVarianceExplanation :one
INSERT INTO fyniq.period_variance_explanation (period_id, account_id, current_amount, previous_amount, explanation, written_by, written_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, period_id, account_id, current_amount, previous_amount, explanation, written_by, written_at;

-- name: LatestVarianceExplanations :many
-- Each account's latest explanation of the period's variance (FR-GL-14): a later one replaces an earlier one.
SELECT DISTINCT ON (account_id) id, period_id, account_id, current_amount, previous_amount, explanation, written_by, written_at
FROM fyniq.period_variance_explanation WHERE period_id = $1
ORDER BY account_id, written_at DESC, id DESC;
