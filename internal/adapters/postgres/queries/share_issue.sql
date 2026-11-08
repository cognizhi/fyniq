-- name: InsertShareIssue :one
-- The amount's minor units come from its currency; an unknown currency inserts nothing.
INSERT INTO fyniq.share_issue (id, holder_id, class_account_id, shares, price, currency, minor_units, amount, consideration, resolution_refs,
                               lodgement_ref, effective_on, settlement_id, entry_id, recorded_by, recorded_at, class_id)
SELECT sqlc.arg(id)::uuid, sqlc.arg(holder_id)::uuid, sqlc.arg(class_account_id)::uuid, sqlc.arg(shares)::numeric, sqlc.arg(price)::numeric,
       c.code, c.minor_units, sqlc.arg(amount)::numeric, sqlc.arg(consideration)::text, sqlc.arg(resolution_refs)::text,
       sqlc.narg(lodgement_ref)::text, sqlc.arg(effective_on)::date, sqlc.narg(settlement_id)::uuid, sqlc.narg(entry_id)::uuid,
       sqlc.arg(recorded_by)::uuid, sqlc.arg(recorded_at)::timestamptz, sqlc.narg(class_id)::uuid
FROM fyniq.currencies c WHERE c.code = sqlc.arg(currency)::text
RETURNING *;

-- name: ShareIssues :many
SELECT * FROM fyniq.share_issue ORDER BY effective_on, id;

-- name: ShareIssueOfSettlement :many
SELECT * FROM fyniq.share_issue WHERE settlement_id = sqlc.arg(settlement_id);

-- name: InsertHoldingMovement :one
INSERT INTO fyniq.holding_movement (id, kind, transaction_id, share_issue_id, reverses_id, holder_id, class_account_id, class_id, shares,
                                    effective_on, recorded_by, recorded_at)
VALUES (sqlc.arg(id)::uuid, sqlc.arg(kind)::text, sqlc.narg(transaction_id)::uuid, sqlc.narg(share_issue_id)::uuid,
        sqlc.narg(reverses_id)::uuid, sqlc.arg(holder_id)::uuid, sqlc.arg(class_account_id)::uuid, sqlc.narg(class_id)::uuid,
        sqlc.arg(shares)::numeric, sqlc.arg(effective_on)::date, sqlc.arg(recorded_by)::uuid, sqlc.arg(recorded_at)::timestamptz)
RETURNING *;

-- name: HoldingMovements :many
SELECT * FROM fyniq.holding_movement ORDER BY effective_on, id;

-- name: HoldingMovementsOfTransaction :many
SELECT * FROM fyniq.holding_movement WHERE transaction_id = sqlc.arg(transaction_id) ORDER BY id;
