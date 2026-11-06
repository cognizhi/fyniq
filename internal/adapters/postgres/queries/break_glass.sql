-- name: InsertBreakGlass :one
-- It lasts 24 hours; the cap's minor units come from its currency, and an unknown currency inserts nothing.
INSERT INTO fyniq.break_glass (started_by, reason, started_at, expires_at, currency, minor_units, cap)
SELECT sqlc.arg(started_by)::uuid, sqlc.arg(reason)::text, sqlc.arg(started_at)::timestamptz,
       sqlc.arg(started_at)::timestamptz + interval '24 hours', c.code, c.minor_units, sqlc.arg(cap)::numeric
FROM fyniq.currencies c WHERE c.code = sqlc.arg(currency)::text
RETURNING *;

-- name: EndBreakGlass :one
UPDATE fyniq.break_glass SET ended_at = sqlc.arg(ended_at)::timestamptz, ended_by = sqlc.arg(ended_by)::uuid, version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version) AND ended_at IS NULL
RETURNING *;

-- name: GetBreakGlass :one
SELECT * FROM fyniq.break_glass WHERE id = sqlc.arg(id);

-- name: ListBreakGlass :many
SELECT * FROM fyniq.break_glass ORDER BY started_at, id;

-- name: InsertBreakGlassUse :one
-- The total's minor units come from its currency; an unknown currency inserts nothing.
INSERT INTO fyniq.break_glass_use (break_glass_id, batch_id, batch_version, used_by, used_at, currency, minor_units, total)
SELECT sqlc.arg(break_glass_id)::uuid, sqlc.arg(batch_id)::uuid, sqlc.arg(batch_version)::integer, sqlc.arg(used_by)::uuid,
       sqlc.arg(used_at)::timestamptz, c.code, c.minor_units, sqlc.arg(total)::numeric
FROM fyniq.currencies c WHERE c.code = sqlc.arg(currency)::text
RETURNING *;

-- name: ListBreakGlassUses :many
SELECT * FROM fyniq.break_glass_use ORDER BY used_at, id;
