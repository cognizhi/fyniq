-- name: InsertPeriod :one
INSERT INTO fyniq.period (starts_on, ends_on) VALUES ($1, $2)
RETURNING id, starts_on, ends_on, status, closed_revision, version;

-- name: GetPeriod :one
SELECT id, starts_on, ends_on, status, closed_revision, version FROM fyniq.period WHERE id = $1;

-- name: LockLedger :one
-- ENG-47 step 2 for close and reopen: the organisation's revision row, FOR UPDATE.
SELECT fyniq.lock_ledger()::bigint AS revision;

-- name: LockPeriod :one
-- ENG-47 step 3: after the ledger lock.
SELECT id, starts_on, ends_on, status, closed_revision, version FROM fyniq.period WHERE id = $1 FOR UPDATE;

-- name: SetPeriodStatus :one
-- period_guard records closed_revision under the ledger lock.
UPDATE fyniq.period
SET status = sqlc.arg(status), version = version + 1
WHERE id = sqlc.arg(id)
RETURNING id, starts_on, ends_on, status, closed_revision, version;
