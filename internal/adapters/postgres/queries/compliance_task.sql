-- name: RaiseRunTask :one
INSERT INTO fyniq.compliance_task (finding_key, title, detail, owner_role, owner_user, due_on, escalated, source, run_id, created_at)
VALUES (sqlc.arg(finding_key), sqlc.arg(title), sqlc.arg(detail), sqlc.narg(owner_role), sqlc.narg(owner_user), sqlc.narg(due_on),
  sqlc.arg(escalated), 'agent_run', sqlc.arg(run_id), sqlc.arg(created_at))
RETURNING *;

-- name: RaiseProposalTask :one
INSERT INTO fyniq.compliance_task (finding_key, title, detail, owner_role, owner_user, due_on, source, proposal_id, created_at)
VALUES (sqlc.arg(finding_key), sqlc.arg(title), sqlc.arg(detail), sqlc.narg(owner_role), sqlc.narg(owner_user), sqlc.narg(due_on),
  'ai_proposal', sqlc.arg(proposal_id), sqlc.arg(created_at))
RETURNING *;

-- name: GetComplianceTask :one
SELECT * FROM fyniq.compliance_task WHERE id = sqlc.arg(id);

-- name: OpenComplianceTask :one
SELECT * FROM fyniq.compliance_task WHERE finding_key = sqlc.arg(finding_key) AND status = 'open';

-- name: ComplianceTasksPage :many
SELECT * FROM fyniq.compliance_task
WHERE (cardinality(sqlc.arg(statuses)::text[]) = 0 OR status = ANY (sqlc.arg(statuses)::text[]))
  AND (sqlc.arg(key_prefix)::text = '' OR starts_with(finding_key, sqlc.arg(key_prefix)::text))
  AND (sqlc.arg(after)::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR id < sqlc.arg(after)::uuid)
ORDER BY id DESC
LIMIT sqlc.arg(max_rows);

-- name: UpdateComplianceTask :one
UPDATE fyniq.compliance_task SET title = sqlc.arg(title), detail = sqlc.arg(detail), owner_role = sqlc.narg(owner_role),
  owner_user = sqlc.narg(owner_user), due_on = sqlc.narg(due_on), escalated = sqlc.arg(escalated), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: ClearComplianceTask :one
UPDATE fyniq.compliance_task SET status = 'cleared', closed_at = sqlc.arg(closed_at), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: CloseComplianceTask :one
UPDATE fyniq.compliance_task SET status = sqlc.arg(status), reason = sqlc.arg(reason), closed_at = sqlc.arg(closed_at),
  closed_by = sqlc.arg(closed_by), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;
