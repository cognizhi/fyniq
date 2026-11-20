-- name: ListCashSnapshots :many
SELECT * FROM fyniq.cash_snapshot ORDER BY week_of, id;

-- name: InsertCashSnapshot :one
INSERT INTO fyniq.cash_snapshot (week_of, taken_on, currency, ledger_revision, projection, taken_at)
VALUES (sqlc.arg(week_of), sqlc.arg(taken_on), sqlc.arg(currency), sqlc.arg(ledger_revision), sqlc.arg(projection), sqlc.arg(taken_at))
RETURNING *;
