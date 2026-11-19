-- Cost rates and billing rates (p1-projects task 3.1).

-- name: CostRates :many
SELECT * FROM fyniq.person_cost_rate ORDER BY effective_from, user_id NULLS FIRST, id;

-- name: InsertCostRate :one
INSERT INTO fyniq.person_cost_rate (user_id, role_name, effective_from, hourly_rate, currency, minor_units, created_by)
SELECT sqlc.narg(user_id), sqlc.arg(role_name), sqlc.arg(effective_from), sqlc.arg(hourly_rate)::numeric, c.code, c.minor_units, sqlc.arg(by)
  FROM fyniq.currencies c WHERE c.code = sqlc.arg(currency)
RETURNING *;

-- name: BillingRates :many
SELECT * FROM fyniq.billing_rate
 WHERE project_id = sqlc.arg(project_id) OR (sqlc.narg(customer_id)::uuid IS NOT NULL AND customer_id = sqlc.narg(customer_id)::uuid)
 ORDER BY effective_from, id;

-- name: InsertBillingRate :one
INSERT INTO fyniq.billing_rate (project_id, customer_id, user_id, role_name, effective_from, hourly_rate, currency, minor_units, created_by)
SELECT sqlc.narg(project_id), sqlc.narg(customer_id), sqlc.narg(user_id), sqlc.narg(role_name), sqlc.arg(effective_from), sqlc.arg(hourly_rate)::numeric,
       c.code, c.minor_units, sqlc.arg(by)
  FROM fyniq.currencies c WHERE c.code = sqlc.arg(currency)
RETURNING *;
