-- name: InsertEvidence :one
INSERT INTO fyniq.evidence (document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256,
  source, data_class, staging_key, uploaded_by, nric_flagged)
VALUES (sqlc.arg(document_id), sqlc.arg(evidence_type), sqlc.narg(note), sqlc.narg(relates_on), sqlc.arg(file_name), sqlc.arg(media_type),
  sqlc.arg(size_bytes), sqlc.arg(sha256), sqlc.arg(original_sha256), sqlc.arg(source), sqlc.arg(data_class), sqlc.arg(staging_key),
  sqlc.arg(uploaded_by), sqlc.arg(nric_flagged))
RETURNING id, document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class,
  staging_key, uploaded_by, uploaded_at, state, removed_at, locked_key, retain_until, finalised_at, version, nric_flagged;

-- name: GetEvidence :one
SELECT id, document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class,
  staging_key, uploaded_by, uploaded_at, state, removed_at, locked_key, retain_until, finalised_at, version, nric_flagged
FROM fyniq.evidence WHERE id = sqlc.arg(id);

-- name: LockEvidence :one
SELECT id, document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class,
  staging_key, uploaded_by, uploaded_at, state, removed_at, locked_key, retain_until, finalised_at, version, nric_flagged
FROM fyniq.evidence WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: PageDocumentEvidence :many
SELECT id, document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class,
  staging_key, uploaded_by, uploaded_at, state, removed_at, locked_key, retain_until, finalised_at, version, nric_flagged
FROM fyniq.evidence WHERE document_id = sqlc.arg(document_id) AND id > sqlc.arg(after)::uuid ORDER BY id LIMIT sqlc.arg(page_size);

-- name: RemoveEvidence :one
UPDATE fyniq.evidence SET state = 'removed', removed_at = sqlc.arg(removed_at), version = version + 1
WHERE id = sqlc.arg(id)
RETURNING id, document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class,
  staging_key, uploaded_by, uploaded_at, state, removed_at, locked_key, retain_until, finalised_at, version, nric_flagged;

-- name: FinaliseEvidence :one
UPDATE fyniq.evidence SET state = 'final', locked_key = sqlc.arg(locked_key), retain_until = sqlc.arg(retain_until),
  finalised_at = sqlc.arg(finalised_at), version = version + 1
WHERE id = sqlc.arg(id)
RETURNING id, document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class,
  staging_key, uploaded_by, uploaded_at, state, removed_at, locked_key, retain_until, finalised_at, version, nric_flagged;

-- name: UnfinalisedEvidence :many
SELECT e.id, e.document_id, e.evidence_type, e.note, e.relates_on, e.file_name, e.media_type, e.size_bytes, e.sha256, e.original_sha256,
  e.source, e.data_class, e.staging_key, e.uploaded_by, e.uploaded_at, e.state, e.removed_at, e.locked_key, e.retain_until,
  e.finalised_at, e.version, e.nric_flagged
FROM fyniq.evidence e JOIN fyniq.document d ON d.organisation_id = e.organisation_id AND d.id = e.document_id
WHERE e.state = 'attached' AND d.status <> 'draft' ORDER BY e.id LIMIT sqlc.arg(page_size);

-- name: LiveStagingKeys :many
SELECT staging_key FROM fyniq.evidence WHERE state = 'attached' AND staging_key = ANY(sqlc.arg(keys)::text[]);

-- name: KnownLockedKeys :many
SELECT locked_key::text FROM fyniq.evidence WHERE locked_key = ANY(sqlc.arg(keys)::text[]);

-- name: PageFinalEvidence :many
SELECT id, document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class,
  staging_key, uploaded_by, uploaded_at, state, removed_at, locked_key, retain_until, finalised_at, version, nric_flagged
FROM fyniq.evidence WHERE state = 'final' AND id > sqlc.arg(after)::uuid ORDER BY id LIMIT sqlc.arg(page_size);

-- name: EvidenceByHash :many
SELECT id, document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class,
  staging_key, uploaded_by, uploaded_at, state, removed_at, locked_key, retain_until, finalised_at, version, nric_flagged
FROM fyniq.evidence
WHERE (original_sha256 = sqlc.arg(hash) OR sha256 = sqlc.arg(hash)) AND state <> 'removed'
ORDER BY uploaded_at, id LIMIT 100;

-- name: EvidenceCounts :many
SELECT document_id, count(*)::integer AS files FROM fyniq.evidence
 WHERE document_id = ANY(sqlc.arg(documents)::uuid[]) AND removed_at IS NULL
 GROUP BY document_id;

-- name: SearchEvidence :many
-- Files on documents of the types given, removed ones aside, newest record date first (a record without one last),
-- then newest file, after the cursor's (date, id).
SELECT e.id, e.document_id, e.evidence_type, e.note, e.relates_on, e.file_name, e.media_type, e.size_bytes, e.sha256, e.original_sha256,
  e.source, e.data_class, e.staging_key, e.uploaded_by, e.uploaded_at, e.state, e.removed_at, e.locked_key, e.retain_until,
  e.finalised_at, e.version, e.nric_flagged, d.type AS document_type, d.status AS document_status,
  coalesce(n.number, '')::text AS document_number, coalesce(v.payload->>'date', '')::text AS document_date
FROM fyniq.evidence e
JOIN fyniq.document d ON d.organisation_id = e.organisation_id AND d.id = e.document_id
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
LEFT JOIN fyniq.document_number n ON n.organisation_id = d.organisation_id AND n.id = d.id
WHERE e.removed_at IS NULL AND d.type = ANY(sqlc.arg(types)::text[])
  AND (sqlc.narg(kind)::text IS NULL OR strpos(lower(e.evidence_type), lower(sqlc.narg(kind)::text)) > 0)
  AND (sqlc.narg(date_from)::text IS NULL OR coalesce(v.payload->>'date', '') >= sqlc.narg(date_from)::text)
  AND (sqlc.narg(date_to)::text IS NULL OR (v.payload->>'date' IS NOT NULL AND v.payload->>'date' <= sqlc.narg(date_to)::text))
  AND (sqlc.narg(uploaded_by)::text IS NULL OR e.uploaded_by = sqlc.narg(uploaded_by)::text)
  AND (sqlc.narg(after_id)::uuid IS NULL
       OR (coalesce(v.payload->>'date', ''), e.id) < (sqlc.arg(after_date)::text, sqlc.narg(after_id)::uuid))
ORDER BY coalesce(v.payload->>'date', '') DESC, e.id DESC
LIMIT sqlc.arg(page_size);
