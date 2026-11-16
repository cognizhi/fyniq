-- name: RecordAICall :one
SELECT fyniq.record_ai_call(sqlc.arg(run), sqlc.arg(agent), sqlc.arg(route_version)::integer, sqlc.arg(step)::integer,
  sqlc.narg(fell_back_from)::integer, sqlc.arg(connection), sqlc.arg(model_requested), sqlc.narg(model_reported), sqlc.narg(version_reported),
  sqlc.arg(actor), sqlc.arg(classes)::text[], sqlc.arg(outcome), sqlc.narg(detail), sqlc.arg(input)::bigint, sqlc.arg(output)::bigint,
  sqlc.arg(cache_read)::bigint, sqlc.arg(cache_write)::bigint, sqlc.arg(reasoning)::bigint, sqlc.narg(cost)::numeric, sqlc.narg(currency),
  sqlc.narg(price), sqlc.narg(cost_base)::numeric, sqlc.arg(latency_ms)::integer, sqlc.arg(started_at))::uuid AS id;

-- name: AICallsOfRun :many
SELECT id, run_id, agent, route_version, step, fell_back_from, connection_id, model_requested, model_reported, version_reported, actor,
  classes, outcome, detail, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens, coalesce(cost::text, '')::text AS cost, currency,
  price_id, coalesce(cost_base::text, '')::text AS cost_base, latency_ms, started_at, recorded_at
FROM fyniq.ai_call WHERE run_id = sqlc.arg(run) ORDER BY started_at, step, id;

-- name: AICallUsage :many
SELECT connection_id, model_requested, agent, actor,
  count(*) FILTER (WHERE outcome NOT IN ('skipped', 'stopped')) AS requests,
  count(*) FILTER (WHERE outcome = 'ok') AS served,
  count(*) FILTER (WHERE outcome = 'ok' AND fell_back_from IS NOT NULL) AS fallbacks,
  count(*) FILTER (WHERE outcome IN ('busy', 'timeout', 'not_found', 'refused', 'failed')) AS errors,
  count(*) FILTER (WHERE outcome = 'skipped') AS skipped,
  coalesce(sum(input_tokens), 0)::bigint AS input_tokens, coalesce(sum(output_tokens), 0)::bigint AS output_tokens,
  coalesce(sum(cache_read_tokens), 0)::bigint AS cache_read_tokens, coalesce(sum(cache_write_tokens), 0)::bigint AS cache_write_tokens,
  coalesce(sum(reasoning_tokens), 0)::bigint AS reasoning_tokens,
  coalesce(sum(cost_base), 0)::text AS cost_base,
  count(*) FILTER (WHERE outcome = 'ok' AND cost_base IS NULL) AS unpriced
FROM fyniq.ai_call
WHERE started_at >= sqlc.arg(from_at) AND started_at < sqlc.arg(to_at)
GROUP BY connection_id, model_requested, agent, actor
ORDER BY connection_id, model_requested, agent, actor;

-- name: AICallsPage :many
SELECT id, run_id, agent, route_version, step, fell_back_from, connection_id, model_requested, model_reported, version_reported, actor,
  classes, outcome, detail, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens, coalesce(cost::text, '')::text AS cost, currency,
  price_id, coalesce(cost_base::text, '')::text AS cost_base, latency_ms, started_at, recorded_at
FROM fyniq.ai_call
WHERE (sqlc.narg(actor)::text IS NULL OR actor = sqlc.narg(actor)::text)
  AND (sqlc.narg(agent)::text IS NULL OR agent = sqlc.narg(agent)::text)
  AND (sqlc.narg(run)::uuid IS NULL OR run_id = sqlc.narg(run)::uuid)
  AND started_at >= sqlc.arg(from_at) AND started_at < sqlc.arg(to_at)
  AND (sqlc.narg(before_at)::timestamptz IS NULL OR (started_at, id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY started_at DESC, id DESC
LIMIT sqlc.arg(page_size)::integer;
