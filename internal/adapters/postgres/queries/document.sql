-- name: InsertDocument :one
INSERT INTO fyniq.document (type) VALUES ($1) RETURNING id, type, status, current_version;

-- name: InsertDocumentVersion :exec
INSERT INTO fyniq.document_version (document_id, version, payload) VALUES ($1, $2, $3);

-- name: GetDocument :one
SELECT d.id, d.type, d.status, d.current_version, v.payload, n.number
FROM fyniq.document d
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
LEFT JOIN fyniq.document_number n ON n.organisation_id = d.organisation_id AND n.id = d.id
WHERE d.id = $1;

-- name: LockDocument :one
-- ENG-47 step 1: the source document a command changes, before the ledger lock.
SELECT d.id, d.type, d.status, d.current_version, v.payload, n.number
FROM fyniq.document d
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
LEFT JOIN fyniq.document_number n ON n.organisation_id = d.organisation_id AND n.id = d.id
WHERE d.id = $1
FOR UPDATE OF d;

-- name: SetDocumentStatus :exec
UPDATE fyniq.document SET status = sqlc.arg(status) WHERE id = sqlc.arg(id);

-- name: GetDocumentStatusForUpdate :one
SELECT status FROM fyniq.document WHERE id = $1 FOR UPDATE;
