-- name: BankFeed :one
SELECT money_account_id, connector_id, provider_account, cursor, synced_at, last_error, version FROM fyniq.bank_feed
WHERE money_account_id = sqlc.arg(money_account_id) FOR UPDATE;

-- name: BankFeeds :many
SELECT money_account_id, connector_id, provider_account, cursor, synced_at, last_error, version FROM fyniq.bank_feed
ORDER BY money_account_id;

-- name: InsertBankFeed :one
INSERT INTO fyniq.bank_feed (money_account_id, connector_id, provider_account, cursor, synced_at, last_error)
VALUES (sqlc.arg(money_account_id), sqlc.arg(connector_id), sqlc.arg(provider_account), sqlc.arg(cursor), sqlc.arg(synced_at), sqlc.narg(last_error))
RETURNING money_account_id, connector_id, provider_account, cursor, synced_at, last_error, version;

-- name: UpdateBankFeed :one
UPDATE fyniq.bank_feed SET connector_id = sqlc.arg(connector_id), provider_account = sqlc.arg(provider_account), cursor = sqlc.arg(cursor),
  synced_at = sqlc.arg(synced_at), last_error = sqlc.narg(last_error), version = version + 1
WHERE money_account_id = sqlc.arg(money_account_id) AND version = sqlc.arg(version)
RETURNING money_account_id, connector_id, provider_account, cursor, synced_at, last_error, version;

-- name: OrganisationConnectorRoute :one
SELECT r.organisation_id::uuid AS organisation_id, r.connector::text AS connector, r.state::text AS state
FROM fyniq.organisation_connector_route(sqlc.arg(id)::uuid) AS r;
