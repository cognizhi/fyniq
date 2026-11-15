-- name: QueueAgentRun :one
-- A slot's run, queued once: a second queueing of the same agent, trigger and slot finds nothing to insert.
INSERT INTO fyniq.agent_run (agent, skill_version, skill_sha256, trigger, slot_at, acts_for, requested_by, queued_at)
VALUES (sqlc.arg(agent), sqlc.arg(skill_version), sqlc.arg(skill_sha256), sqlc.arg(trigger), sqlc.arg(slot_at), sqlc.arg(acts_for),
  sqlc.narg(requested_by), sqlc.arg(queued_at))
ON CONFLICT (organisation_id, agent, trigger, slot_at) DO NOTHING
RETURNING *;

-- name: AgentRunBySlot :one
SELECT * FROM fyniq.agent_run WHERE agent = sqlc.arg(agent) AND trigger = sqlc.arg(trigger) AND slot_at = sqlc.arg(slot_at);

-- name: GetAgentRun :one
SELECT * FROM fyniq.agent_run WHERE id = sqlc.arg(id);

-- name: AgentRunsPage :many
SELECT * FROM fyniq.agent_run
WHERE (sqlc.arg(agent)::text = '' OR agent = sqlc.arg(agent)::text)
  AND (cardinality(sqlc.arg(statuses)::text[]) = 0 OR status = ANY (sqlc.arg(statuses)::text[]))
  AND (sqlc.narg(finished_since)::timestamptz IS NULL OR finished_at >= sqlc.narg(finished_since)::timestamptz)
  AND (sqlc.arg(after)::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR id < sqlc.arg(after)::uuid)
ORDER BY id DESC
LIMIT sqlc.arg(max_rows);

-- name: LastDoneAgentRun :one
SELECT * FROM fyniq.agent_run WHERE agent = sqlc.arg(agent) AND status = 'done' ORDER BY finished_at DESC, id DESC LIMIT 1;

-- name: StartAgentRun :one
UPDATE fyniq.agent_run SET status = 'running', started_at = sqlc.arg(started_at), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: SkipAgentRun :one
UPDATE fyniq.agent_run SET status = 'skipped', reason = sqlc.arg(reason), finished_at = sqlc.arg(finished_at), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: FailAgentRun :one
UPDATE fyniq.agent_run SET status = 'failed', reason = sqlc.arg(reason), finished_at = sqlc.arg(finished_at), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: FinishAgentRun :one
UPDATE fyniq.agent_run SET status = 'done', finished_at = sqlc.arg(finished_at), fingerprint = sqlc.arg(fingerprint),
  ledger_revision = sqlc.arg(ledger_revision), pack_versions = sqlc.arg(pack_versions), kb_snapshot = sqlc.arg(kb_snapshot),
  findings = sqlc.arg(findings), summary = sqlc.arg(summary), model_used = sqlc.arg(model_used), model = sqlc.narg(model),
  model_text = sqlc.narg(model_text), tokens = sqlc.arg(tokens), tool_calls = sqlc.arg(tool_calls), tools = sqlc.narg(tools), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;
