-- name: GetKPIInput :one
SELECT * FROM fyniq.kpi_input WHERE month = sqlc.arg(month);

-- name: KPIInputsBetween :many
SELECT * FROM fyniq.kpi_input WHERE month BETWEEN sqlc.arg(from_month) AND sqlc.arg(to_month) ORDER BY month;

-- name: InsertKPIInput :one
INSERT INTO fyniq.kpi_input (month, paying_customers, new_customers, lost_customers, headcount, entered_by, entered_at)
VALUES (sqlc.arg(month), sqlc.narg(paying_customers), sqlc.narg(new_customers), sqlc.narg(lost_customers), sqlc.narg(headcount),
        sqlc.arg(entered_by), sqlc.arg(entered_at))
RETURNING *;

-- name: UpdateKPIInput :one
UPDATE fyniq.kpi_input SET paying_customers = sqlc.narg(paying_customers), new_customers = sqlc.narg(new_customers),
  lost_customers = sqlc.narg(lost_customers), headcount = sqlc.narg(headcount), entered_by = sqlc.arg(entered_by),
  entered_at = sqlc.arg(entered_at), version = version + 1
WHERE month = sqlc.arg(month) AND version = sqlc.arg(version)
RETURNING *;
