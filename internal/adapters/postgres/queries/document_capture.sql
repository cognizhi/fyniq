-- name: InsertDocumentCapture :one
INSERT INTO fyniq.document_capture (source, file_name, media_type, size_bytes, sha256, blob_key, message_id, inbound_file_id, owner_user_id, captured_by, status, duplicate_of, created_at, nric_flagged)
VALUES (sqlc.arg(source), sqlc.arg(file_name), sqlc.arg(media_type), sqlc.arg(size_bytes), sqlc.arg(sha256), sqlc.arg(blob_key), sqlc.narg(message_id), sqlc.narg(inbound_file_id),
  sqlc.narg(owner_user_id), sqlc.arg(captured_by), sqlc.arg(status), sqlc.narg(duplicate_of), sqlc.arg(created_at), sqlc.arg(nric_flagged))
RETURNING id;

-- name: GetDocumentCapture :one
SELECT id, source, file_name, media_type, size_bytes, sha256, blob_key, message_id, inbound_file_id, owner_user_id, captured_by, status, kind, reading, reading_model,
  reading_prompt, readings, read_at, checks, duplicate_of, draft_kind, draft_id, drafted_by, drafted_at, dismissed_by, dismissed_at, dismiss_reason, last_error, created_at, version, nric_flagged
FROM fyniq.document_capture WHERE id = sqlc.arg(id);

-- name: LockDocumentCapture :one
SELECT id, source, file_name, media_type, size_bytes, sha256, blob_key, message_id, inbound_file_id, owner_user_id, captured_by, status, kind, reading, reading_model,
  reading_prompt, readings, read_at, checks, duplicate_of, draft_kind, draft_id, drafted_by, drafted_at, dismissed_by, dismissed_at, dismiss_reason, last_error, created_at, version, nric_flagged
FROM fyniq.document_capture WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: ListDocumentCaptures :many
SELECT id, source, file_name, media_type, size_bytes, sha256, blob_key, message_id, inbound_file_id, owner_user_id, captured_by, status, kind, reading, reading_model,
  reading_prompt, readings, read_at, checks, duplicate_of, draft_kind, draft_id, drafted_by, drafted_at, dismissed_by, dismissed_at, dismiss_reason, last_error, created_at, version, nric_flagged
FROM fyniq.document_capture
WHERE (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text)
  AND (owner_user_id IS NULL OR owner_user_id = sqlc.narg(viewer)::uuid)
  AND (sqlc.arg(before)::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR id < sqlc.arg(before)::uuid)
ORDER BY id DESC LIMIT sqlc.arg(max)::int;

-- name: CaptureOfHash :many
SELECT id FROM fyniq.document_capture WHERE sha256 = sqlc.arg(sha256) AND status <> 'dismissed' AND (owner_user_id IS NULL OR owner_user_id = sqlc.narg(viewer)::uuid)
ORDER BY id LIMIT 1;

-- name: SetDocumentCaptureReading :one
UPDATE fyniq.document_capture SET status = sqlc.arg(status), kind = sqlc.narg(kind), reading = sqlc.narg(reading), reading_model = sqlc.narg(reading_model),
  reading_prompt = sqlc.narg(reading_prompt), readings = readings + 1, read_at = sqlc.arg(read_at), checks = sqlc.narg(checks), last_error = NULL, version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version) RETURNING id;

-- name: SetDocumentCaptureStatus :one
UPDATE fyniq.document_capture SET status = sqlc.arg(status), last_error = sqlc.narg(last_error), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version) RETURNING id;

-- name: DraftDocumentCapture :one
UPDATE fyniq.document_capture SET status = 'drafted', draft_kind = sqlc.arg(draft_kind), draft_id = sqlc.arg(draft_id), drafted_by = sqlc.arg(drafted_by),
  drafted_at = sqlc.arg(drafted_at), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version) RETURNING id;

-- name: DismissDocumentCapture :one
UPDATE fyniq.document_capture SET status = 'dismissed', dismissed_by = sqlc.arg(dismissed_by), dismissed_at = sqlc.arg(dismissed_at), dismiss_reason = sqlc.narg(dismiss_reason),
  version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version) RETURNING id;

-- name: CaptureOfInboundFile :many
SELECT id FROM fyniq.document_capture WHERE inbound_file_id = sqlc.arg(inbound_file_id) LIMIT 1;
