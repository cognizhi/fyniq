-- name: DashboardLayoutOfRole :one
SELECT * FROM fyniq.dashboard_layout WHERE scope = 'role' AND role = sqlc.arg(role);

-- name: DashboardLayoutOfUser :one
SELECT * FROM fyniq.dashboard_layout WHERE scope = 'user' AND user_id = sqlc.arg(user_id);

-- name: InsertDashboardLayout :one
INSERT INTO fyniq.dashboard_layout (scope, role, user_id, tiles, updated_by, updated_at)
VALUES (sqlc.arg(scope), sqlc.narg(role), sqlc.narg(user_id), sqlc.arg(tiles), sqlc.arg(updated_by), sqlc.arg(updated_at))
RETURNING *;

-- name: UpdateDashboardLayout :one
UPDATE fyniq.dashboard_layout SET tiles = sqlc.arg(tiles), updated_by = sqlc.arg(updated_by), updated_at = sqlc.arg(updated_at), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;
