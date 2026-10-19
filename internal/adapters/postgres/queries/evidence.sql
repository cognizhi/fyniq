-- name: InsertEvidence :one
INSERT INTO fyniq.evidence (document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256,
  source, data_class, staging_key, uploaded_by)
VALUES (sqlc.arg(document_id), sqlc.arg(evidence_type), sqlc.narg(note), sqlc.narg(relates_on), sqlc.arg(file_name), sqlc.arg(media_type),
  sqlc.arg(size_bytes), sqlc.arg(sha256), sqlc.arg(original_sha256), sqlc.arg(source), sqlc.arg(data_class), sqlc.arg(staging_key),
  sqlc.arg(uploaded_by))
RETURNING id, document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class,
  staging_key, uploaded_by, uploaded_at, state, removed_at, locked_key, retain_until, finalised_at, version;

-- name: GetEvidence :one
SELECT id, document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class,
  staging_key, uploaded_by, uploaded_at, state, removed_at, locked_key, retain_until, finalised_at, version
FROM fyniq.evidence WHERE id = sqlc.arg(id);

-- name: LockEvidence :one
SELECT id, document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class,
  staging_key, uploaded_by, uploaded_at, state, removed_at, locked_key, retain_until, finalised_at, version
FROM fyniq.evidence WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: PageDocumentEvidence :many
SELECT id, document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class,
  staging_key, uploaded_by, uploaded_at, state, removed_at, locked_key, retain_until, finalised_at, version
FROM fyniq.evidence WHERE document_id = sqlc.arg(document_id) AND id > sqlc.arg(after)::uuid ORDER BY id LIMIT sqlc.arg(page_size);

-- name: RemoveEvidence :one
UPDATE fyniq.evidence SET state = 'removed', removed_at = sqlc.arg(removed_at), version = version + 1
WHERE id = sqlc.arg(id)
RETURNING id, document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class,
  staging_key, uploaded_by, uploaded_at, state, removed_at, locked_key, retain_until, finalised_at, version;

-- name: FinaliseEvidence :one
UPDATE fyniq.evidence SET state = 'final', locked_key = sqlc.arg(locked_key), retain_until = sqlc.arg(retain_until),
  finalised_at = sqlc.arg(finalised_at), version = version + 1
WHERE id = sqlc.arg(id)
RETURNING id, document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class,
  staging_key, uploaded_by, uploaded_at, state, removed_at, locked_key, retain_until, finalised_at, version;

-- name: UnfinalisedEvidence :many
SELECT e.id, e.document_id, e.evidence_type, e.note, e.relates_on, e.file_name, e.media_type, e.size_bytes, e.sha256, e.original_sha256,
  e.source, e.data_class, e.staging_key, e.uploaded_by, e.uploaded_at, e.state, e.removed_at, e.locked_key, e.retain_until,
  e.finalised_at, e.version
FROM fyniq.evidence e JOIN fyniq.document d ON d.organisation_id = e.organisation_id AND d.id = e.document_id
WHERE e.state = 'attached' AND d.status <> 'draft' ORDER BY e.id LIMIT sqlc.arg(page_size);

-- name: LiveStagingKeys :many
SELECT staging_key FROM fyniq.evidence WHERE state = 'attached' AND staging_key = ANY(sqlc.arg(keys)::text[]);

-- name: KnownLockedKeys :many
SELECT locked_key::text FROM fyniq.evidence WHERE locked_key = ANY(sqlc.arg(keys)::text[]);

-- name: PageFinalEvidence :many
SELECT id, document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class,
  staging_key, uploaded_by, uploaded_at, state, removed_at, locked_key, retain_until, finalised_at, version
FROM fyniq.evidence WHERE state = 'final' AND id > sqlc.arg(after)::uuid ORDER BY id LIMIT sqlc.arg(page_size);
