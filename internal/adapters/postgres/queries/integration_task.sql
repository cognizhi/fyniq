-- name: InsertIntegrationTask :one
INSERT INTO fyniq.integration_task (client_id, external_ref, title, detail, due_on, link_url, created_by, created_at, updated_at)
VALUES (sqlc.arg(client_id), sqlc.arg(external_ref), sqlc.arg(title), sqlc.arg(detail), sqlc.narg(due_on), sqlc.narg(link_url), sqlc.arg(created_by),
  sqlc.arg(at), sqlc.arg(at))
ON CONFLICT (organisation_id, client_id, external_ref) DO NOTHING
RETURNING *;

-- name: GetIntegrationTask :one
SELECT * FROM fyniq.integration_task WHERE id = sqlc.arg(id);

-- name: GetIntegrationTaskByRef :one
SELECT * FROM fyniq.integration_task WHERE client_id = sqlc.arg(client_id) AND external_ref = sqlc.arg(external_ref);

-- name: IntegrationTasksPage :many
SELECT * FROM fyniq.integration_task
WHERE (cardinality(sqlc.arg(statuses)::text[]) = 0 OR status = ANY (sqlc.arg(statuses)::text[]))
  AND (sqlc.arg(client_id)::text = '' OR client_id = sqlc.arg(client_id)::text)
  AND (sqlc.arg(after)::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR id < sqlc.arg(after)::uuid)
ORDER BY id DESC
LIMIT sqlc.arg(max_rows);

-- name: UpdateIntegrationTask :one
UPDATE fyniq.integration_task SET title = sqlc.arg(title), detail = sqlc.arg(detail), due_on = sqlc.narg(due_on), link_url = sqlc.narg(link_url),
  status = sqlc.arg(status), closed_at = sqlc.narg(closed_at), updated_at = sqlc.arg(at), version = version + 1
WHERE id = sqlc.arg(id) AND client_id = sqlc.arg(client_id) AND version = sqlc.arg(version)
RETURNING *;
