-- name: InsertOwnerSuccession :one
-- It is due 7 days after it starts.
INSERT INTO fyniq.owner_succession (document_id, outgoing_membership_id, outgoing_user_id, successor_membership_id, reason, evidence_kind,
  started_by, started_at, due_at)
VALUES (sqlc.arg(document_id)::uuid, sqlc.arg(outgoing_membership_id)::uuid, sqlc.arg(outgoing_user_id)::uuid,
        sqlc.narg(successor_membership_id)::uuid, sqlc.arg(reason)::text, sqlc.arg(evidence_kind)::text, sqlc.arg(started_by)::uuid,
        sqlc.arg(started_at)::timestamptz, sqlc.arg(started_at)::timestamptz + interval '7 days')
RETURNING *;

-- name: EndOwnerSuccession :one
UPDATE fyniq.owner_succession SET status = sqlc.arg(status)::text, ended_by = sqlc.arg(ended_by)::uuid,
       ended_at = sqlc.arg(ended_at)::timestamptz, version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version) AND status = 'notice'
RETURNING *;

-- name: GetOwnerSuccession :one
SELECT * FROM fyniq.owner_succession WHERE id = sqlc.arg(id);

-- name: ListOwnerSuccessions :many
SELECT * FROM fyniq.owner_succession ORDER BY started_at DESC, id DESC;
