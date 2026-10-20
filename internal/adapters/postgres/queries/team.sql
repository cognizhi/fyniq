-- name: ListTeamModes :many
SELECT id, version, mode, request_id, created_by, created_at
  FROM fyniq.team_mode
 ORDER BY version;

-- name: InsertTeamMode :one
INSERT INTO fyniq.team_mode (version, mode, request_id, created_by, created_at)
VALUES (sqlc.arg(version), sqlc.arg(mode), sqlc.arg(request_id), sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING id, version, mode, request_id, created_by, created_at;

-- name: ListSoDOverrides :many
SELECT id, team_mode_version, user_id, conflict, reason, request_id, created_by, created_at
  FROM fyniq.sod_override
 ORDER BY team_mode_version, created_at, id;

-- name: InsertSoDOverride :one
INSERT INTO fyniq.sod_override (team_mode_version, user_id, conflict, reason, request_id, created_by, created_at)
VALUES (sqlc.arg(team_mode_version), sqlc.arg(user_id), sqlc.arg(conflict), sqlc.arg(reason), sqlc.arg(request_id),
        sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING id, team_mode_version, user_id, conflict, reason, request_id, created_by, created_at;
