-- name: InsertAIGrant :one
INSERT INTO fyniq.ai_grant (token_hash, user_id, turn_id, issued_at, expires_at)
VALUES (sqlc.arg(token_hash), sqlc.arg(user_id), sqlc.arg(turn_id), sqlc.arg(issued_at), sqlc.arg(expires_at))
RETURNING *;

-- name: EndAIGrant :execrows
UPDATE fyniq.ai_grant SET ended_at = sqlc.arg(ended_at) WHERE turn_id = sqlc.arg(turn_id) AND ended_at IS NULL;

-- name: AIGrantFor :one
-- A nil user means no live grant.
SELECT coalesce(g.user_id, '00000000-0000-0000-0000-000000000000')::uuid AS user_id,
       coalesce(g.turn_id, '00000000-0000-0000-0000-000000000000')::uuid AS turn_id,
       coalesce(g.roles, '{}')::text[] AS roles,
       coalesce(g.expires_at, 'epoch')::timestamptz AS expires_at
  FROM fyniq.ai_grant_for(sqlc.arg(org)::uuid, sqlc.arg(token_hash)::bytea, sqlc.arg(now)::timestamptz) AS g;

-- name: AIGrantClasses :one
-- A live grant's classes: none when the turn has no live grant.
SELECT classes FROM fyniq.ai_grant WHERE turn_id = sqlc.arg(turn_id) AND ended_at IS NULL AND expires_at > sqlc.arg(now);

-- name: WidenAIGrant :execrows
UPDATE fyniq.ai_grant SET classes = ARRAY(SELECT DISTINCT unnest(classes || sqlc.arg(classes)::text[]) ORDER BY 1)
 WHERE turn_id = sqlc.arg(turn_id) AND ended_at IS NULL AND expires_at > sqlc.arg(now);
