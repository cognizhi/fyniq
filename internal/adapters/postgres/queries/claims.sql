-- Claim categories (p1-expense-claims task 1.1).

-- name: InsertClaimCategory :one
INSERT INTO fyniq.claim_category (name, account_id, mileage, currency, minor_units, line_limit, receipt_above, attendees_above,
  created_by, created_at, changed_by, changed_at)
SELECT sqlc.arg(name), sqlc.arg(account_id), sqlc.arg(mileage), c.code, c.minor_units, sqlc.narg(line_limit)::numeric,
  sqlc.narg(receipt_above)::numeric, sqlc.narg(attendees_above)::numeric, sqlc.arg(by), sqlc.arg(at), sqlc.arg(by), sqlc.arg(at)
  FROM fyniq.currencies c WHERE c.code = sqlc.arg(currency)
RETURNING *;

-- name: UpdateClaimCategory :one
UPDATE fyniq.claim_category
   SET name = sqlc.arg(name), account_id = sqlc.arg(account_id), line_limit = sqlc.narg(line_limit)::numeric,
       receipt_above = sqlc.narg(receipt_above)::numeric, attendees_above = sqlc.narg(attendees_above)::numeric,
       active = sqlc.arg(active), changed_by = sqlc.arg(by), changed_at = sqlc.arg(at), version = version + 1
 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: ClaimCategories :many
SELECT * FROM fyniq.claim_category ORDER BY active DESC, lower(name), id;

-- name: ClaimCategory :one
SELECT * FROM fyniq.claim_category WHERE id = sqlc.arg(id);
