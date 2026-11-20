-- name: GetProjectRevenueElection :one
SELECT project_id, method, COALESCE(estimated_cost::text, '')::text AS estimated_cost, version, set_by, set_at
FROM fyniq.project_revenue_election WHERE project_id = sqlc.arg(project_id);

-- name: ListProjectRevenueElections :many
SELECT project_id, method, COALESCE(estimated_cost::text, '')::text AS estimated_cost, version, set_by, set_at
FROM fyniq.project_revenue_election ORDER BY project_id;

-- name: InsertProjectRevenueElection :one
INSERT INTO fyniq.project_revenue_election (project_id, method, estimated_cost, set_by, set_at)
VALUES (sqlc.arg(project_id), sqlc.arg(method), sqlc.narg(estimated_cost)::numeric, sqlc.arg(set_by), sqlc.arg(set_at))
RETURNING project_id, method, COALESCE(estimated_cost::text, '')::text AS estimated_cost, version, set_by, set_at;

-- name: UpdateProjectRevenueElection :one
UPDATE fyniq.project_revenue_election
SET method = sqlc.arg(method), estimated_cost = sqlc.narg(estimated_cost)::numeric, set_by = sqlc.arg(set_by), set_at = sqlc.arg(set_at), version = version + 1
WHERE project_id = sqlc.arg(project_id) AND version = sqlc.arg(version)
RETURNING project_id, method, COALESCE(estimated_cost::text, '')::text AS estimated_cost, version, set_by, set_at;

-- name: ProjectRevenueRunsOf :many
SELECT id, project_id, month, journal_id, adjustment::text AS adjustment, earned::text AS earned, created_by, created_at
FROM fyniq.project_revenue_run WHERE project_id = sqlc.arg(project_id) ORDER BY month;

-- name: InsertProjectRevenueRun :one
INSERT INTO fyniq.project_revenue_run (project_id, month, journal_id, adjustment, earned, created_by, created_at)
VALUES (sqlc.arg(project_id), sqlc.arg(month), sqlc.arg(journal_id), sqlc.arg(adjustment)::numeric, sqlc.arg(earned)::numeric, sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING id, project_id, month, journal_id, adjustment::text AS adjustment, earned::text AS earned, created_by, created_at;
