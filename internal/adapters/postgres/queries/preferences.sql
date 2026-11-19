-- name: InsertUserPreference :one
INSERT INTO fyniq.user_preference (user_id, mode, ledger_view, theme, contrast, density, motion, shortcuts_on, shortcut_keys, number_format, date_format, set_at)
VALUES (sqlc.arg(user_id), sqlc.narg(mode), sqlc.narg(ledger_view), sqlc.narg(theme), sqlc.narg(contrast), sqlc.narg(density), sqlc.narg(motion),
        sqlc.narg(shortcuts_on), sqlc.arg(shortcut_keys)::jsonb, sqlc.narg(number_format), sqlc.narg(date_format), sqlc.arg(set_at))
RETURNING id, user_id, mode, ledger_view, theme, contrast, density, motion, shortcuts_on, shortcut_keys, number_format, date_format, set_at;

-- name: LatestUserPreference :many
-- The preferences in force: the latest save.
SELECT id, user_id, mode, ledger_view, theme, contrast, density, motion, shortcuts_on, shortcut_keys, number_format, date_format, set_at
FROM fyniq.user_preference WHERE user_id = sqlc.arg(user_id)
ORDER BY set_at DESC, id DESC LIMIT 1;
