-- name: InsertAIGrant :one
INSERT INTO fyniq.ai_grant (token_hash, user_id, turn_id, run_id, tools, task_types, issued_at, expires_at, propose)
VALUES (sqlc.arg(token_hash), sqlc.arg(user_id), sqlc.narg(turn_id), sqlc.narg(run_id), sqlc.narg(tools), sqlc.narg(task_types), sqlc.arg(issued_at),
  sqlc.arg(expires_at), sqlc.arg(propose))
RETURNING *;

-- name: EndAIGrant :execrows
-- A grant's origin is its turn or its run.
UPDATE fyniq.ai_grant SET ended_at = sqlc.arg(ended_at)
 WHERE (turn_id = sqlc.arg(origin)::uuid OR run_id = sqlc.arg(origin)::uuid) AND ended_at IS NULL;

-- name: AIGrantFor :one
-- A nil user means no live grant.
SELECT coalesce(g.user_id, '00000000-0000-0000-0000-000000000000')::uuid AS user_id,
       coalesce(g.turn_id, '00000000-0000-0000-0000-000000000000')::uuid AS turn_id,
       coalesce(g.roles, '{}')::text[] AS roles,
       coalesce(g.expires_at, 'epoch')::timestamptz AS expires_at,
       coalesce(g.propose, false)::boolean AS propose,
       coalesce(g.run_id, '00000000-0000-0000-0000-000000000000')::uuid AS run_id,
       g.tools::text[] AS tools,
       g.task_types::text[] AS task_types,
       coalesce(g.agent, '')::text AS agent
  FROM fyniq.ai_grant_for(sqlc.arg(org)::uuid, sqlc.arg(token_hash)::bytea, sqlc.arg(now)::timestamptz) AS g;

-- name: AIGrantClasses :one
-- A live grant's classes: none when its turn or run has no live grant.
SELECT classes FROM fyniq.ai_grant
 WHERE (turn_id = sqlc.arg(origin)::uuid OR run_id = sqlc.arg(origin)::uuid) AND ended_at IS NULL AND expires_at > sqlc.arg(now);

-- name: WidenAIGrant :execrows
UPDATE fyniq.ai_grant SET classes = ARRAY(SELECT DISTINCT unnest(classes || sqlc.arg(classes)::text[]) ORDER BY 1)
 WHERE (turn_id = sqlc.arg(origin)::uuid OR run_id = sqlc.arg(origin)::uuid) AND ended_at IS NULL AND expires_at > sqlc.arg(now);
