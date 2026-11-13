-- The full tenant export's reads (FR-MIG-04, design D10), run as the worker under its grants and row-level security.

-- name: TenantTables :many
SELECT t.table_name::text AS name, t.column_names::text[] AS columns FROM fyniq.tenant_tables() t;

-- name: TenantRows :many
SELECT r::text AS row FROM fyniq.tenant_rows(sqlc.arg(table_name)::text, sqlc.narg(after)::uuid, sqlc.arg(row_limit)::integer) AS r;
