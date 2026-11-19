-- name: MCPRegistrationOpen :one
SELECT fyniq.mcp_registration_open(sqlc.arg(now)::timestamptz)::boolean AS open;

-- name: RegisterMCPClient :one
SELECT fyniq.mcp_client_register(sqlc.arg(id)::text, sqlc.arg(name)::text, sqlc.arg(uris)::text[], sqlc.arg(network)::text, sqlc.arg(now)::timestamptz,
  sqlc.arg(per_hour)::integer, sqlc.arg(per_day)::integer, sqlc.arg(pending_cap)::integer)::text AS refused;

-- name: MCPClientStatus :one
SELECT fyniq.mcp_client_status(sqlc.arg(id)::text)::text AS status;

-- name: DecideMCPClient :one
SELECT fyniq.mcp_client_decide(sqlc.arg(id)::text, sqlc.arg(status)::text, sqlc.arg(now)::timestamptz)::boolean AS decided;

-- name: TouchMCPClient :exec
SELECT fyniq.mcp_client_touch(sqlc.arg(id)::text, sqlc.arg(now)::timestamptz);

-- name: PendingMCPClients :many
SELECT * FROM fyniq.mcp_clients_pending();

-- name: ExpireMCPClients :many
SELECT fyniq.mcp_clients_expire(sqlc.arg(now)::timestamptz, sqlc.arg(pending)::interval, sqlc.arg(unused)::interval)::text AS client_id;

-- name: NamedMCPClients :many
SELECT * FROM fyniq.mcp_clients_named(sqlc.arg(ids)::text[]);
