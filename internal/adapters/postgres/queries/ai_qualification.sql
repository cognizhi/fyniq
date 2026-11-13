-- name: LatestAIQualifications :many
SELECT DISTINCT ON (connection_id, model, agent) id, agent, connection_id, model, version, suite, suite_version, threshold::text AS threshold,
  cases, passed, result, run_id, ran_by, ran_at, out_of_date_at, out_of_date_why
FROM fyniq.ai_qualification WHERE connection_id = sqlc.arg(connection_id)
ORDER BY connection_id, model, agent, ran_at DESC, id DESC;

-- name: InsertAIQualification :one
INSERT INTO fyniq.ai_qualification (agent, connection_id, model, version, suite, suite_version, threshold, cases, passed, result, run_id,
  ran_by, ran_at)
VALUES (sqlc.arg(agent), sqlc.arg(connection_id), sqlc.arg(model), sqlc.arg(version), sqlc.arg(suite), sqlc.arg(suite_version),
  sqlc.arg(threshold)::numeric, sqlc.arg(cases), sqlc.arg(passed), sqlc.arg(result), sqlc.arg(run_id), sqlc.arg(ran_by), sqlc.arg(ran_at))
RETURNING id;

-- name: AIQualificationOutOfDate :execrows
UPDATE fyniq.ai_qualification SET out_of_date_at = sqlc.arg(at), out_of_date_why = sqlc.arg(why)
WHERE id = sqlc.arg(id) AND out_of_date_at IS NULL;

-- name: DueAIQualifications :many
SELECT * FROM (
  SELECT DISTINCT ON (connection_id, model, agent) id, agent, connection_id, model, version, suite, suite_version, threshold::text AS threshold,
    cases, passed, result, run_id, ran_by, ran_at, out_of_date_at, out_of_date_why
  FROM fyniq.ai_qualification ORDER BY connection_id, model, agent, ran_at DESC, id DESC) latest
WHERE result = 'pass' AND (ran_at < sqlc.arg(before) OR out_of_date_at IS NOT NULL)
ORDER BY ran_at;
