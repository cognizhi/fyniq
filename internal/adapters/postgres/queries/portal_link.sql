-- name: InsertPortalLink :one
INSERT INTO fyniq.portal_link (contact_id, label, token_hash, expires_at, created_by, created_at)
VALUES (sqlc.arg(contact_id), sqlc.arg(label), sqlc.arg(token_hash), sqlc.arg(expires_at), sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING id, contact_id, label, expires_at, created_by, created_at, first_opened_at, revoked_at, revoked_by;

-- name: GetPortalLink :one
SELECT id, contact_id, label, expires_at, created_by, created_at, first_opened_at, revoked_at, revoked_by
FROM fyniq.portal_link WHERE id = sqlc.arg(id);

-- name: PortalLinksOfContact :many
SELECT id, contact_id, label, expires_at, created_by, created_at, first_opened_at, revoked_at, revoked_by
FROM fyniq.portal_link WHERE contact_id = sqlc.arg(contact_id) ORDER BY created_at DESC, id DESC;

-- name: OpenPortalLink :one
-- A link's first opening; no row when it was opened before.
UPDATE fyniq.portal_link SET first_opened_at = sqlc.arg(at)
WHERE id = sqlc.arg(id) AND first_opened_at IS NULL
RETURNING id, contact_id, label, expires_at, created_by, created_at, first_opened_at, revoked_at, revoked_by;

-- name: RevokePortalLink :one
-- A link's revocation; no row when it was revoked before.
UPDATE fyniq.portal_link SET revoked_at = now(), revoked_by = sqlc.arg(revoked_by)
WHERE id = sqlc.arg(id) AND revoked_at IS NULL
RETURNING id, contact_id, label, expires_at, created_by, created_at, first_opened_at, revoked_at, revoked_by;

-- name: PortalLinkRoute :one
SELECT r.organisation_id::uuid AS organisation_id, r.link_id::uuid AS link_id
FROM fyniq.portal_link_route(sqlc.arg(token_hash)::text) AS r;
