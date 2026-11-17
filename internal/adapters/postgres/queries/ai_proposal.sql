-- name: InsertAIProposal :one
INSERT INTO fyniq.ai_proposal (task_type, task_version, task_sha256, payload, payload_sha256, item_count, rationale, evidence, risk_tier,
  required_approvals, agent, requested_by, turn_id, run_id, model, prompt, tools_sha256, proposed_at, expires_at)
VALUES (sqlc.arg(task_type), sqlc.arg(task_version), sqlc.arg(task_sha256), sqlc.arg(payload), sqlc.arg(payload_sha256), sqlc.arg(item_count),
  sqlc.arg(rationale), sqlc.arg(evidence), sqlc.arg(risk_tier), sqlc.arg(required_approvals), sqlc.arg(agent), sqlc.arg(requested_by),
  sqlc.narg(turn_id), sqlc.narg(run_id), sqlc.arg(model), sqlc.arg(prompt), sqlc.arg(tools_sha256), sqlc.arg(proposed_at), sqlc.arg(expires_at))
RETURNING *;

-- name: GetAIProposal :one
SELECT * FROM fyniq.ai_proposal WHERE id = sqlc.arg(id);

-- name: AIProposalsPage :many
SELECT * FROM fyniq.ai_proposal
WHERE (cardinality(sqlc.arg(statuses)::text[]) = 0 OR status = ANY (sqlc.arg(statuses)::text[]))
  AND (sqlc.narg(requested_by)::uuid IS NULL OR requested_by = sqlc.narg(requested_by)::uuid)
  AND (sqlc.narg(turn_id)::uuid IS NULL OR turn_id = sqlc.narg(turn_id)::uuid)
  AND (sqlc.arg(after)::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR id < sqlc.arg(after)::uuid)
ORDER BY id DESC
LIMIT sqlc.arg(max_rows);

-- name: InvalidAIProposal :one
UPDATE fyniq.ai_proposal SET status = 'invalid', reason = sqlc.arg(reason), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: PendingAIProposal :one
UPDATE fyniq.ai_proposal SET status = 'pending', impact = sqlc.arg(impact), base_versions = sqlc.arg(base_versions),
  approval_request_id = sqlc.narg(approval_request_id), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: ApproveAIProposal :one
UPDATE fyniq.ai_proposal SET status = 'approved', decided_by = sqlc.arg(decided_by), decided_at = sqlc.arg(decided_at),
  approved_items = sqlc.narg(approved_items), approved_payload = sqlc.arg(approved_payload), approved_sha256 = sqlc.arg(approved_sha256),
  version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: RejectAIProposal :one
UPDATE fyniq.ai_proposal SET status = 'rejected', decided_by = sqlc.arg(decided_by), decided_at = sqlc.arg(decided_at),
  reason = sqlc.arg(reason), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: SupersedeAIProposal :one
UPDATE fyniq.ai_proposal SET status = 'superseded', superseded_by = sqlc.arg(superseded_by), decided_at = sqlc.arg(decided_at),
  version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: ExpireAIProposal :one
UPDATE fyniq.ai_proposal SET status = 'expired', decided_at = sqlc.arg(decided_at), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: StartAIProposal :one
UPDATE fyniq.ai_proposal SET status = 'executing', started_at = sqlc.arg(started_at), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: FinishAIProposal :one
UPDATE fyniq.ai_proposal SET status = sqlc.arg(status), reason = sqlc.arg(reason), execution = sqlc.arg(execution),
  finished_at = sqlc.arg(finished_at), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;
