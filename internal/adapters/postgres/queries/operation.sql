-- name: InsertOperation :one
INSERT INTO fyniq.operation (kind, job_id, resource, requested_by, requested_at)
VALUES (sqlc.arg(kind), sqlc.arg(job_id), sqlc.arg(resource), sqlc.arg(requested_by), sqlc.arg(requested_at))
RETURNING id;

-- name: GetOperation :one
SELECT o.id, o.kind, o.job_id, o.resource, o.requested_by, o.requested_at, j.state AS job_state, j.updated_at AS job_updated
FROM fyniq.operation o
JOIN fyniq.outbox j ON j.organisation_id = o.organisation_id AND j.id = o.job_id
WHERE o.id = $1;
