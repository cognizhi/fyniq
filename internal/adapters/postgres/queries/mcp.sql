-- name: InsertMCPUserOptIn :one
INSERT INTO fyniq.mcp_user_optin (user_id, opted_in, set_at)
VALUES (sqlc.arg(user_id), sqlc.arg(opted_in), sqlc.arg(set_at))
RETURNING id, user_id, opted_in, set_at;

-- name: LatestMCPUserOptIn :many
-- The state in force: the latest.
SELECT id, user_id, opted_in, set_at FROM fyniq.mcp_user_optin WHERE user_id = sqlc.arg(user_id)
ORDER BY set_at DESC, id DESC LIMIT 1;

-- name: InsertMCPCall :one
INSERT INTO fyniq.mcp_call (user_id, app, app_version, token_id, tool, input_sha256, output_sha256, facts, duration_ms, outcome, code, network, at)
VALUES (sqlc.arg(user_id), sqlc.arg(app), sqlc.arg(app_version), sqlc.arg(token_id), sqlc.arg(tool), sqlc.arg(input_sha256), sqlc.arg(output_sha256),
        sqlc.arg(facts), sqlc.arg(duration_ms), sqlc.arg(outcome), sqlc.arg(code), sqlc.arg(network), sqlc.arg(at))
RETURNING id;

-- name: CountMCPCalls :one
-- The calls a person's app made since a time, and the oldest of them: what a limit counts and when it frees.
SELECT count(*)::bigint AS calls, COALESCE(min(at), to_timestamp(0))::timestamptz AS oldest FROM fyniq.mcp_call
WHERE user_id = sqlc.arg(user_id) AND app = sqlc.arg(app) AND at > sqlc.arg(since) AND at <= sqlc.arg(until);

-- name: ListMCPCalls :many
SELECT id, user_id, app, app_version, token_id, tool, input_sha256, output_sha256, facts, duration_ms, outcome, code, network, at
FROM fyniq.mcp_call WHERE (sqlc.arg(user_id)::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR user_id = sqlc.arg(user_id))
ORDER BY at DESC, id DESC LIMIT sqlc.arg(row_limit);

-- name: MarkMCPSeen :one
-- True when this is the first time: the pair was not there.
WITH ins AS (
  INSERT INTO fyniq.mcp_seen (user_id, kind, value, first_seen) VALUES (sqlc.arg(user_id), sqlc.arg(kind), sqlc.arg(value), sqlc.arg(first_seen))
  ON CONFLICT DO NOTHING RETURNING 1
) SELECT count(*) > 0 AS first FROM ins;
