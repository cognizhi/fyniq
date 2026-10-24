-- name: InsertDocumentLink :one
INSERT INTO fyniq.document_link (document_id, document_version, message_id, token_hash, expires_at, created_by)
VALUES (sqlc.arg(document_id), sqlc.arg(document_version), sqlc.arg(message_id), sqlc.arg(token_hash), sqlc.arg(expires_at), sqlc.arg(created_by))
RETURNING id, document_id, document_version, message_id, expires_at, created_by, created_at, first_viewed_at, revoked_at, revoked_by;

-- name: GetDocumentLink :one
SELECT id, document_id, document_version, message_id, expires_at, created_by, created_at, first_viewed_at, revoked_at, revoked_by
FROM fyniq.document_link WHERE id = sqlc.arg(id);

-- name: DocumentLinksOf :many
-- A document's links, newest first.
SELECT id, document_id, document_version, message_id, expires_at, created_by, created_at, first_viewed_at, revoked_at, revoked_by
FROM fyniq.document_link WHERE document_id = sqlc.arg(document_id)
ORDER BY created_at DESC, id DESC;

-- name: ViewDocumentLink :one
-- A link's first view; no row when it was viewed before.
UPDATE fyniq.document_link SET first_viewed_at = sqlc.arg(at)
WHERE id = sqlc.arg(id) AND first_viewed_at IS NULL
RETURNING id, document_id, document_version, message_id, expires_at, created_by, created_at, first_viewed_at, revoked_at, revoked_by;

-- name: RevokeDocumentLink :one
-- A link's revocation; no row when it was revoked before.
UPDATE fyniq.document_link SET revoked_at = now(), revoked_by = sqlc.arg(revoked_by)
WHERE id = sqlc.arg(id) AND revoked_at IS NULL
RETURNING id, document_id, document_version, message_id, expires_at, created_by, created_at, first_viewed_at, revoked_at, revoked_by;

-- name: DocumentLinkRoute :one
SELECT r.organisation_id::uuid AS organisation_id, r.link_id::uuid AS link_id
FROM fyniq.document_link_route(sqlc.arg(token_hash)::text) AS r;
