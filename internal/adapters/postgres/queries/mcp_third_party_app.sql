-- name: InsertMCPThirdPartyApp :one
INSERT INTO fyniq.mcp_third_party_app (id, client_id, name, publisher, publisher_url, redirect_uris, purpose, processing_location, outside_singapore, inventory_entry,
  registered_by, registered_at)
VALUES (sqlc.arg(id), sqlc.arg(client_id), sqlc.arg(name), sqlc.arg(publisher), sqlc.narg(publisher_url), sqlc.arg(redirect_uris), sqlc.arg(purpose),
  sqlc.arg(processing_location), sqlc.arg(outside_singapore), sqlc.arg(inventory_entry), sqlc.arg(registered_by), sqlc.arg(at))
RETURNING *;

-- name: ListMCPThirdPartyApps :many
SELECT * FROM fyniq.mcp_third_party_app ORDER BY registered_at DESC, id DESC LIMIT 200;

-- name: GetMCPThirdPartyApp :one
SELECT * FROM fyniq.mcp_third_party_app WHERE client_id = sqlc.arg(client_id);

-- name: RemoveMCPThirdPartyApp :one
UPDATE fyniq.mcp_third_party_app SET removed_at = sqlc.arg(at), removed_by = sqlc.arg(by)
WHERE client_id = sqlc.arg(client_id) AND removed_at IS NULL
RETURNING *;
