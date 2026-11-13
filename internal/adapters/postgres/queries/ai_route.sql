-- name: AIAgents :many
SELECT * FROM fyniq.ai_agents ORDER BY position;

-- name: InsertAIRoute :one
INSERT INTO fyniq.ai_route (agent, version, classes, preset, approval_id, made_by, made_at)
VALUES (sqlc.arg(agent), sqlc.arg(version), sqlc.arg(classes)::text[], sqlc.narg(preset), sqlc.narg(approval_id), sqlc.arg(made_by), sqlc.arg(made_at))
RETURNING *;

-- name: InsertAIRouteStep :exec
INSERT INTO fyniq.ai_route_step (route_id, position, connection_id, model, effort, cache)
VALUES (sqlc.arg(route_id), sqlc.arg(position), sqlc.arg(connection_id), sqlc.arg(model), sqlc.arg(effort), sqlc.arg(cache));

-- name: CurrentAIRoutes :many
SELECT DISTINCT ON (agent) * FROM fyniq.ai_route ORDER BY agent, version DESC;

-- name: AIRouteVersions :many
SELECT * FROM fyniq.ai_route WHERE agent = sqlc.arg(agent) ORDER BY version DESC LIMIT sqlc.arg(page_size);

-- name: AIRouteSteps :many
SELECT route_id, position, connection_id, model, effort, cache FROM fyniq.ai_route_step
WHERE route_id = ANY(sqlc.arg(route_ids)::uuid[]) ORDER BY route_id, position;
