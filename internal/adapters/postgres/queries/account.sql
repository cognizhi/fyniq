-- name: InsertAccount :one
INSERT INTO fyniq.account (code, name, type, parent_id, is_group)
VALUES (sqlc.arg(code), sqlc.arg(name), sqlc.arg(type), sqlc.narg(parent_id), sqlc.arg(is_group))
RETURNING id, code, name, type, parent_id, is_group, is_active, version;

-- name: GetAccount :one
SELECT id, code, name, type, parent_id, is_group, is_active, version FROM fyniq.account WHERE id = $1;

-- name: GetAccountByCode :one
SELECT id, code, name, type, parent_id, is_group, is_active, version FROM fyniq.account WHERE code = $1;

-- name: SetAccountActive :one
UPDATE fyniq.account SET is_active = sqlc.arg(is_active), version = version + 1
WHERE id = sqlc.arg(id)
RETURNING id, code, name, type, parent_id, is_group, is_active, version;
