-- name: InsertUserSavedItem :one
INSERT INTO fyniq.user_saved_item (user_id, kind, item_key, state, title, path, query, set_at)
VALUES (sqlc.arg(user_id), sqlc.arg(kind), sqlc.arg(item_key), sqlc.arg(state), sqlc.arg(title), sqlc.arg(path), sqlc.arg(query), sqlc.arg(set_at))
RETURNING id, user_id, kind, item_key, state, title, path, query, set_at;

-- name: LatestUserSavedItems :many
-- What is in force: the latest row of each key, the caller sorts and drops those that are off.
SELECT DISTINCT ON (kind, item_key) id, user_id, kind, item_key, state, title, path, query, set_at
FROM fyniq.user_saved_item WHERE user_id = sqlc.arg(user_id)
ORDER BY kind, item_key, set_at DESC, id DESC;
