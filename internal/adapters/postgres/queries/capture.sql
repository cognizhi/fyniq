-- name: InsertCaptureAddress :one
INSERT INTO fyniq.capture_address (local_part, user_id, created_by, created_at)
VALUES (sqlc.arg(local_part), sqlc.narg(user_id), sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING id, local_part, user_id, created_by, created_at, retired_at;

-- name: LiveCaptureAddresses :many
SELECT id, local_part, user_id, created_by, created_at, retired_at FROM fyniq.capture_address
WHERE retired_at IS NULL ORDER BY user_id NULLS FIRST, created_at;

-- name: LiveCaptureAddressOf :one
SELECT id, local_part, user_id, created_by, created_at, retired_at FROM fyniq.capture_address
WHERE retired_at IS NULL AND user_id IS NOT DISTINCT FROM sqlc.narg(user_id)::uuid FOR UPDATE;

-- name: RetireCaptureAddress :one
UPDATE fyniq.capture_address SET retired_at = sqlc.arg(retired_at), retired_by = sqlc.arg(retired_by)
WHERE id = sqlc.arg(id) AND retired_at IS NULL
RETURNING id, local_part, user_id, created_by, created_at, retired_at;

-- name: CaptureAddressByID :one
SELECT id, local_part, user_id, created_by, created_at, retired_at FROM fyniq.capture_address WHERE id = sqlc.arg(id);

-- name: InsertCaptureSender :one
INSERT INTO fyniq.capture_sender (address, user_id, decision, decided_by, decided_at)
VALUES (sqlc.arg(address), sqlc.narg(user_id), sqlc.arg(decision), sqlc.arg(decided_by), sqlc.arg(decided_at))
RETURNING id, address, user_id, decision, decided_by, decided_at;

-- name: CaptureSendersInForce :many
-- The latest decision about each sender at each address: the one in force.
SELECT DISTINCT ON (address, user_id) id, address, user_id, decision, decided_by, decided_at
FROM fyniq.capture_sender
ORDER BY address, user_id, decided_at DESC, id DESC;

-- name: CaptureRoute :one
SELECT r.organisation_id::uuid AS organisation_id, r.address_id::uuid AS address_id, coalesce(r.user_id::text, '')::text AS user_id
FROM fyniq.capture_route(sqlc.arg(local_part)::text) AS r;
