-- name: InsertRecordComment :one
INSERT INTO fyniq.record_comment (record_type, record_id, author, body, mentions, created_at)
VALUES (sqlc.arg(record_type), sqlc.arg(record_id), sqlc.arg(author), sqlc.arg(body), sqlc.arg(mentions)::uuid[], sqlc.arg(created_at))
RETURNING id, record_type, record_id, author, body, mentions, created_at;

-- name: RecordThread :many
SELECT id, record_type, record_id, author, body, mentions, created_at
FROM fyniq.record_comment
WHERE record_type = sqlc.arg(record_type) AND record_id = sqlc.arg(record_id)
ORDER BY created_at, id
LIMIT sqlc.arg(max_rows);
