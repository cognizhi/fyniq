-- name: RecordAICall :one
SELECT fyniq.record_ai_call(sqlc.arg(run), sqlc.arg(agent), sqlc.arg(route_version)::integer, sqlc.arg(step)::integer,
  sqlc.narg(fell_back_from)::integer, sqlc.arg(connection), sqlc.arg(model_requested), sqlc.narg(model_reported), sqlc.narg(version_reported),
  sqlc.arg(actor), sqlc.arg(classes)::text[], sqlc.arg(outcome), sqlc.narg(detail), sqlc.arg(input)::bigint, sqlc.arg(output)::bigint,
  sqlc.arg(cache_read)::bigint, sqlc.arg(cache_write)::bigint, sqlc.arg(reasoning)::bigint, sqlc.narg(cost)::numeric, sqlc.narg(currency),
  sqlc.narg(price), sqlc.arg(latency_ms)::integer, sqlc.arg(started_at))::uuid AS id;

-- name: AICallsOfRun :many
SELECT id, run_id, agent, route_version, step, fell_back_from, connection_id, model_requested, model_reported, version_reported, actor,
  classes, outcome, detail, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens, coalesce(cost::text, '')::text AS cost, currency,
  price_id, latency_ms, started_at, recorded_at
FROM fyniq.ai_call WHERE run_id = sqlc.arg(run) ORDER BY started_at, step, id;
