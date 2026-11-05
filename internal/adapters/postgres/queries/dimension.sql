-- name: InsertDimension :one
INSERT INTO fyniq.dimension (kind, position, name)
VALUES (sqlc.arg(kind), sqlc.narg(position), sqlc.arg(name))
RETURNING id, kind, position, name, is_active, version, created_at;

-- name: Dimensions :many
-- Department, Project, then the custom dimensions by place (FR-DIM-01).
SELECT id, kind, position, name, is_active, version, created_at
FROM fyniq.dimension
ORDER BY CASE kind WHEN 'department' THEN 0 WHEN 'project' THEN 1 ELSE 2 END, position;

-- name: Dimension :one
SELECT id, kind, position, name, is_active, version, created_at FROM fyniq.dimension WHERE id = $1;

-- name: UpdateDimension :one
UPDATE fyniq.dimension SET name = sqlc.arg(name), is_active = sqlc.arg(is_active), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING id, kind, position, name, is_active, version, created_at;

-- name: InsertDimensionValue :one
INSERT INTO fyniq.dimension_value (dimension_id, code, name)
VALUES (sqlc.arg(dimension_id), sqlc.arg(code), sqlc.arg(name))
RETURNING id, dimension_id, code, name, is_active, version, created_at;

-- name: DimensionValues :many
-- The values of one dimension, or of all when none is named, by dimension as Dimensions orders them, then by code.
SELECT v.id, v.dimension_id, v.code, v.name, v.is_active, v.version, v.created_at
FROM fyniq.dimension_value v
JOIN fyniq.dimension d ON d.organisation_id = v.organisation_id AND d.id = v.dimension_id
WHERE sqlc.narg(dimension_id)::uuid IS NULL OR v.dimension_id = sqlc.narg(dimension_id)::uuid
ORDER BY CASE d.kind WHEN 'department' THEN 0 WHEN 'project' THEN 1 ELSE 2 END, d.position, v.code;

-- name: DimensionValue :one
SELECT id, dimension_id, code, name, is_active, version, created_at FROM fyniq.dimension_value WHERE id = $1;

-- name: UpdateDimensionValue :one
UPDATE fyniq.dimension_value SET name = sqlc.arg(name), is_active = sqlc.arg(is_active), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING id, dimension_id, code, name, is_active, version, created_at;
