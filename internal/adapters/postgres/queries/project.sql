-- Projects: the Project dimension's values with their attributes, members and tasks (p1-projects task 1.1).

-- name: Projects :many
-- Every value of the Project dimension, configured or not, by code; one when an ID is given.
SELECT v.id, v.dimension_id, v.code, v.name, v.is_active, v.version AS value_version,
       (p.id IS NOT NULL)::boolean AS configured,
       p.customer_id, COALESCE(p.currency, '')::text AS currency, COALESCE(p.minor_units, 0)::smallint AS minor_units,
       COALESCE(p.status, 'active')::text AS status, COALESCE(p.billing_method, 'internal')::text AS billing_method,
       p.starts_on, p.ends_on, p.budget_minutes, p.budget_amount, COALESCE(p.version, 0)::integer AS version,
       COALESCE(p.created_at, v.created_at)::timestamptz AS created_at, COALESCE(p.changed_at, v.created_at)::timestamptz AS changed_at
  FROM fyniq.dimension_value v
  JOIN fyniq.dimension d ON d.organisation_id = v.organisation_id AND d.id = v.dimension_id AND d.kind = 'project'
  LEFT JOIN fyniq.project p ON p.organisation_id = v.organisation_id AND p.id = v.id
 WHERE sqlc.narg(id)::uuid IS NULL OR v.id = sqlc.narg(id)::uuid
 ORDER BY lower(v.code), v.id;

-- name: InsertProject :one
INSERT INTO fyniq.project (id, customer_id, currency, minor_units, status, billing_method, starts_on, ends_on,
  budget_minutes, budget_amount)
SELECT sqlc.arg(id), sqlc.narg(customer_id), c.code, c.minor_units, sqlc.arg(status), sqlc.arg(billing_method),
  sqlc.narg(starts_on)::date, sqlc.narg(ends_on)::date, sqlc.narg(budget_minutes)::integer, sqlc.narg(budget_amount)::numeric
  FROM fyniq.currencies c WHERE c.code = sqlc.arg(currency)
RETURNING id;

-- name: UpdateProject :one
UPDATE fyniq.project
   SET customer_id = sqlc.narg(customer_id), status = sqlc.arg(status), billing_method = sqlc.arg(billing_method),
       starts_on = sqlc.narg(starts_on)::date, ends_on = sqlc.narg(ends_on)::date, budget_minutes = sqlc.narg(budget_minutes)::integer,
       budget_amount = sqlc.narg(budget_amount)::numeric, version = version + 1, changed_at = now()
 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING id;

-- name: ProjectMembers :many
SELECT project_id, user_id, is_manager, is_active FROM fyniq.project_member
 WHERE project_id = sqlc.arg(project_id) ORDER BY is_manager DESC, user_id;

-- name: InsertProjectMember :exec
INSERT INTO fyniq.project_member (project_id, user_id, is_manager) VALUES (sqlc.arg(project_id), sqlc.arg(user_id), sqlc.arg(is_manager));

-- name: UpdateProjectMember :exec
UPDATE fyniq.project_member SET is_manager = sqlc.arg(is_manager), is_active = sqlc.arg(is_active)
 WHERE project_id = sqlc.arg(project_id) AND user_id = sqlc.arg(user_id);

-- name: ProjectTasks :many
SELECT * FROM fyniq.project_task WHERE project_id = sqlc.arg(project_id) ORDER BY is_active DESC, lower(name), id;

-- name: ProjectTask :one
SELECT * FROM fyniq.project_task WHERE id = sqlc.arg(id);

-- name: InsertProjectTask :one
INSERT INTO fyniq.project_task (project_id, name, billable) VALUES (sqlc.arg(project_id), sqlc.arg(name), sqlc.arg(billable))
RETURNING *;

-- name: UpdateProjectTask :one
UPDATE fyniq.project_task SET name = sqlc.arg(name), billable = sqlc.arg(billable), is_active = sqlc.arg(is_active),
  version = version + 1, changed_at = now()
 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: ProjectsOfUser :many
SELECT project_id FROM fyniq.project_member WHERE user_id = sqlc.arg(user_id) AND is_active ORDER BY project_id;
