-- name: APIClients :many
SELECT * FROM fyniq.api_client WHERE kind = sqlc.arg(kind) ORDER BY created_at, id;

-- name: GetAPIClient :one
SELECT * FROM fyniq.api_client WHERE id = sqlc.arg(id);

-- name: GetAPIClientForUpdate :one
SELECT * FROM fyniq.api_client WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: InsertAPIClient :one
INSERT INTO fyniq.api_client (id, kind, client_id, name, class, owner_id, scopes, ip_allow, reads_per_minute, writes_per_minute, key_thumbprint,
  expires_at, approval_id, created_by, created_at)
VALUES (sqlc.arg(id), sqlc.arg(kind), sqlc.arg(client_id), sqlc.arg(name), sqlc.arg(class), sqlc.arg(owner_id), sqlc.arg(scopes)::text[],
  sqlc.arg(ip_allow)::text[], sqlc.narg(reads_per_minute), sqlc.narg(writes_per_minute), sqlc.narg(key_thumbprint), sqlc.arg(expires_at),
  sqlc.narg(approval_id), sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING *;

-- name: RevokeAPIClient :one
UPDATE fyniq.api_client SET revoked_at = sqlc.arg(revoked_at), revoked_by = sqlc.arg(revoked_by), revoke_reason = sqlc.arg(revoke_reason),
  version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: APIClientAccess :one
-- An empty kind means a client ID Fyniq does not know.
SELECT coalesce(a.organisation_id, '00000000-0000-0000-0000-000000000000')::uuid AS organisation_id,
       coalesce(a.id, '00000000-0000-0000-0000-000000000000')::uuid AS id, coalesce(a.kind, '')::text AS kind,
       coalesce(a.class, '')::text AS class, coalesce(a.owner_id, '00000000-0000-0000-0000-000000000000')::uuid AS owner_id,
       coalesce(a.owner_active, false)::boolean AS owner_active, coalesce(a.roles, '{}')::text[] AS roles,
       coalesce(a.scopes, '{}')::text[] AS scopes, coalesce(a.ip_allow, '{}')::text[] AS ip_allow,
       coalesce(a.reads_per_minute, 0)::integer AS reads_per_minute, coalesce(a.writes_per_minute, 0)::integer AS writes_per_minute,
       coalesce(a.expires_at, 'epoch')::timestamptz AS expires_at, coalesce(a.revoked, true)::boolean AS revoked
  FROM fyniq.api_client_access(sqlc.arg(client_id)::text, sqlc.arg(now)::timestamptz) AS a;
