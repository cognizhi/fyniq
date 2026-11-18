-- name: InsertMCPUserOptIn :one
INSERT INTO fyniq.mcp_user_optin (user_id, opted_in, set_at)
VALUES (sqlc.arg(user_id), sqlc.arg(opted_in), sqlc.arg(set_at))
RETURNING id, user_id, opted_in, set_at;

-- name: LatestMCPUserOptIn :many
-- The state in force: the latest.
SELECT id, user_id, opted_in, set_at FROM fyniq.mcp_user_optin WHERE user_id = sqlc.arg(user_id)
ORDER BY set_at DESC, id DESC LIMIT 1;
