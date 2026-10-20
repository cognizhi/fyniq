-- name: ListReviewLog :many
SELECT e.id, e.kind, e.object_type, e.object_key, e.summary, e.actor, e.approval_id, e.logged_at,
       r.reviewed_by, r.reviewed_at, r.note
  FROM fyniq.review_log_entry e
  LEFT JOIN fyniq.review_log_review r ON r.organisation_id = e.organisation_id AND r.entry_id = e.id
 ORDER BY e.logged_at, e.id;

-- name: InsertReviewLogEntry :one
INSERT INTO fyniq.review_log_entry (kind, object_type, object_key, summary, actor, approval_id, logged_at)
VALUES (sqlc.arg(kind), sqlc.arg(object_type), sqlc.arg(object_key), sqlc.arg(summary), sqlc.narg(actor), sqlc.narg(approval_id),
        sqlc.arg(logged_at))
RETURNING id;

-- name: InsertReviewLogReview :one
INSERT INTO fyniq.review_log_review (entry_id, reviewed_by, reviewed_at, note)
VALUES (sqlc.arg(entry_id), sqlc.arg(reviewed_by), sqlc.arg(reviewed_at), sqlc.narg(note))
RETURNING id;
