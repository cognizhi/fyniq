-- name: ListSequences :many
SELECT id, document_type, prefix, next_number, digits, gap_free, version FROM fyniq.document_sequence ORDER BY document_type;

-- name: FindSequence :one
SELECT id, document_type, prefix, next_number, digits, gap_free, version FROM fyniq.document_sequence WHERE document_type = sqlc.arg(document_type);

-- name: LockSequence :one
-- ENG-47 step 1a: a document type's sequence, after the documents' locks and before the ledger revision.
SELECT id, document_type, prefix, next_number, digits, gap_free, version FROM fyniq.document_sequence WHERE document_type = sqlc.arg(document_type) FOR UPDATE;

-- name: InsertSequence :one
INSERT INTO fyniq.document_sequence (document_type, prefix, next_number, digits, gap_free)
VALUES (sqlc.arg(document_type), sqlc.arg(prefix), sqlc.arg(next_number), sqlc.arg(digits), sqlc.arg(gap_free))
ON CONFLICT (organisation_id, document_type) DO NOTHING
RETURNING id, document_type, prefix, next_number, digits, gap_free, version;

-- name: UpdateSequence :one
UPDATE fyniq.document_sequence SET prefix = sqlc.arg(prefix), next_number = sqlc.arg(next_number), digits = sqlc.arg(digits),
  gap_free = sqlc.arg(gap_free), version = version + 1
WHERE document_type = sqlc.arg(document_type)
RETURNING id, document_type, prefix, next_number, digits, gap_free, version;

-- name: AdvanceSequence :exec
UPDATE fyniq.document_sequence SET next_number = next_number + 1 WHERE id = sqlc.arg(id);

-- name: InsertDocumentNumber :exec
INSERT INTO fyniq.document_number (id, document_type, number) VALUES (sqlc.arg(id), sqlc.arg(document_type), sqlc.arg(number));

-- name: GetDocumentNumber :one
SELECT number FROM fyniq.document_number WHERE id = sqlc.arg(id);
