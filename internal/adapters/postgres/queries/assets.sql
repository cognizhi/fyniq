-- name: InsertAssetSource :exec
INSERT INTO fyniq.fixed_asset_source (asset_id, source_id, source_version, line, account_id)
VALUES (sqlc.arg(asset_id), sqlc.arg(source_id), sqlc.arg(source_version), sqlc.arg(line), sqlc.arg(account_id));

-- name: AssetSource :one
SELECT * FROM fyniq.fixed_asset_source WHERE asset_id = sqlc.arg(asset_id);

-- name: AssetSourcesOfDocument :many
SELECT * FROM fyniq.fixed_asset_source WHERE source_id = sqlc.arg(source_id) ORDER BY line;

-- name: AssetSources :many
SELECT * FROM fyniq.fixed_asset_source ORDER BY source_id, line;
