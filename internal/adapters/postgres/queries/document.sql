-- name: InsertDocument :one
INSERT INTO fyniq.document (type) VALUES ($1) RETURNING id, type, status, current_version;

-- name: InsertDocumentVersion :exec
INSERT INTO fyniq.document_version (document_id, version, payload, created_by) VALUES ($1, $2, $3, $4);

-- name: SetDocumentVersion :exec
-- A new version becomes the current one; document_guard refuses going back.
UPDATE fyniq.document SET current_version = sqlc.arg(version) WHERE id = sqlc.arg(id);

-- name: GetDocument :one
SELECT d.id, d.type, d.status, d.current_version, v.payload, n.number, v.created_by, d.posted_by, d.posted_at
FROM fyniq.document d
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
LEFT JOIN fyniq.document_number n ON n.organisation_id = d.organisation_id AND n.id = d.id
WHERE d.id = $1;

-- name: LockDocument :one
-- ENG-47 step 1: the source document a command changes, before the ledger lock.
SELECT d.id, d.type, d.status, d.current_version, v.payload, n.number, v.created_by, d.posted_by, d.posted_at
FROM fyniq.document d
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
LEFT JOIN fyniq.document_number n ON n.organisation_id = d.organisation_id AND n.id = d.id
WHERE d.id = $1
FOR UPDATE OF d;

-- name: ListDocuments :many
-- Documents of a type at their current version, newest date first (a draft without a date last), then newest ID,
-- after the cursor's (date, id). The text matches the memo or the number, as typed: no wildcards.
SELECT d.id, d.type, d.status, d.current_version, v.payload, n.number, v.created_by, d.posted_by, d.posted_at,
       coalesce(v.payload->>'date', '')::text AS date
FROM fyniq.document d
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
LEFT JOIN fyniq.document_number n ON n.organisation_id = d.organisation_id AND n.id = d.id
WHERE d.type = sqlc.arg(type)
  AND (sqlc.narg(status)::text IS NULL OR d.status = sqlc.narg(status)::text)
  AND (sqlc.narg(date_from)::text IS NULL OR coalesce(v.payload->>'date', '') >= sqlc.narg(date_from)::text)
  AND (sqlc.narg(date_to)::text IS NULL OR (v.payload->>'date' IS NOT NULL AND v.payload->>'date' <= sqlc.narg(date_to)::text))
  AND (sqlc.narg(search)::text IS NULL
       OR strpos(lower(coalesce(v.payload->>'memo', '')), lower(sqlc.narg(search)::text)) > 0
       OR strpos(lower(coalesce(n.number, '')), lower(sqlc.narg(search)::text)) > 0)
  AND (sqlc.narg(after_id)::uuid IS NULL
       OR (coalesce(v.payload->>'date', ''), d.id) < (sqlc.arg(after_date)::text, sqlc.narg(after_id)::uuid))
ORDER BY coalesce(v.payload->>'date', '') DESC, d.id DESC
LIMIT sqlc.arg(page_size);

-- name: SetDocumentStatus :exec
-- Posting records who posted and when (FY053); reversing leaves them as they are.
UPDATE fyniq.document
SET status = sqlc.arg(status),
    posted_by = coalesce(sqlc.narg(posted_by)::text, posted_by),
    posted_at = coalesce(sqlc.narg(posted_at)::timestamptz, posted_at)
WHERE id = sqlc.arg(id);

-- name: GetDocumentStatusForUpdate :one
SELECT status FROM fyniq.document WHERE id = $1 FOR UPDATE;

-- name: InsertOpeningBalance :exec
-- The organisation's one opening balances document (FR-GL-06): its key is the organisation.
INSERT INTO fyniq.opening_balance (document_id) VALUES ($1);

-- name: GetOpeningBalance :one
SELECT d.id, d.type, d.status, d.current_version, v.payload, n.number, v.created_by, d.posted_by, d.posted_at
FROM fyniq.opening_balance o
JOIN fyniq.document d ON d.organisation_id = o.organisation_id AND d.id = o.document_id
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
LEFT JOIN fyniq.document_number n ON n.organisation_id = d.organisation_id AND n.id = d.id;
