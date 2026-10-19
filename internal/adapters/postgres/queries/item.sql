-- name: InsertItem :one
INSERT INTO fyniq.item (code, name, description, type, unit, revenue_account_id, gst_code)
VALUES (sqlc.arg(code), sqlc.arg(name), sqlc.narg(description), sqlc.arg(type), sqlc.narg(unit), sqlc.narg(revenue_account_id), sqlc.narg(gst_code))
RETURNING id, code, name, description, type, unit, revenue_account_id, gst_code, is_active, version;

-- name: GetItem :one
SELECT id, code, name, description, type, unit, revenue_account_id, gst_code, is_active, version FROM fyniq.item WHERE id = sqlc.arg(id);

-- name: LockItem :one
SELECT id, code, name, description, type, unit, revenue_account_id, gst_code, is_active, version FROM fyniq.item WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: UpdateItem :one
UPDATE fyniq.item SET name = sqlc.arg(name), description = sqlc.narg(description), type = sqlc.arg(type), unit = sqlc.narg(unit),
  revenue_account_id = sqlc.narg(revenue_account_id), gst_code = sqlc.narg(gst_code), is_active = sqlc.arg(is_active), version = version + 1
WHERE id = sqlc.arg(id)
RETURNING id, code, name, description, type, unit, revenue_account_id, gst_code, is_active, version;

-- name: PageItems :many
SELECT id, code, name, description, type, unit, revenue_account_id, gst_code, is_active, version FROM fyniq.item WHERE code > sqlc.arg(after)::text ORDER BY code LIMIT sqlc.arg(page_size);

-- name: ItemPrices :many
SELECT item_id, currency, unit_price::text AS unit_price FROM fyniq.item_price WHERE item_id = ANY(sqlc.arg(item_ids)::uuid[]) ORDER BY item_id, currency;

-- name: InsertItemPrice :exec
INSERT INTO fyniq.item_price (item_id, currency, unit_price) VALUES (sqlc.arg(item_id), sqlc.arg(currency), sqlc.arg(unit_price)::numeric);

-- name: DeleteItemPrices :exec
DELETE FROM fyniq.item_price WHERE item_id = sqlc.arg(item_id);
