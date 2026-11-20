-- name: ListCapTableGrants :many
SELECT * FROM fyniq.cap_table_grant ORDER BY granted_at DESC, id DESC;

-- name: LiveCapTableGrantOf :one
SELECT * FROM fyniq.cap_table_grant WHERE user_id = sqlc.arg(user_id) AND revoked_at IS NULL;

-- name: GetCapTableGrant :one
SELECT * FROM fyniq.cap_table_grant WHERE id = sqlc.arg(id);

-- name: InsertCapTableGrant :one
INSERT INTO fyniq.cap_table_grant (user_id, reason, granted_by, granted_at) VALUES (sqlc.arg(user_id), sqlc.arg(reason), sqlc.arg(granted_by), sqlc.arg(granted_at))
RETURNING *;

-- name: RevokeCapTableGrant :one
UPDATE fyniq.cap_table_grant SET revoked_by = sqlc.arg(revoked_by), revoked_at = sqlc.arg(revoked_at), revoke_reason = sqlc.arg(revoke_reason)
WHERE id = sqlc.arg(id) AND revoked_at IS NULL
RETURNING *;
