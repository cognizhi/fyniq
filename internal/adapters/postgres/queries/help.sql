-- name: InsertUserHelpState :one
INSERT INTO fyniq.user_help_state (user_id, tour, step, set_at)
VALUES (sqlc.arg(user_id), sqlc.arg(tour), sqlc.arg(step), sqlc.arg(set_at))
RETURNING id, user_id, tour, step, set_at;

-- name: LatestUserHelpState :many
-- The state in force: the latest.
SELECT id, user_id, tour, step, set_at FROM fyniq.user_help_state WHERE user_id = sqlc.arg(user_id)
ORDER BY set_at DESC, id DESC LIMIT 1;
