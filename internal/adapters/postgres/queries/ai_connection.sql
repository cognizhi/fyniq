-- name: AIConnections :many
SELECT * FROM fyniq.ai_connection ORDER BY name, id;

-- name: GetAIConnection :one
SELECT * FROM fyniq.ai_connection WHERE id = sqlc.arg(id);

-- name: GetAIConnectionForUpdate :one
SELECT * FROM fyniq.ai_connection WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: InsertAIConnection :one
INSERT INTO fyniq.ai_connection (id, name, connector, preset, auth, configuration, host, self_hosted, credential_ref, credential_set_at,
  billing_mode, quota, created_by, created_at, changed_by, changed_at)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(connector), sqlc.narg(preset), sqlc.arg(auth), sqlc.arg(configuration)::jsonb, sqlc.arg(host),
  sqlc.arg(self_hosted), sqlc.narg(credential_ref), sqlc.narg(credential_set_at), sqlc.arg(billing_mode), sqlc.narg(quota)::jsonb,
  sqlc.arg(created_by), sqlc.arg(created_at), sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING *;

-- name: UpdateAIConnection :one
UPDATE fyniq.ai_connection SET name = sqlc.arg(name), preset = sqlc.narg(preset), auth = sqlc.arg(auth),
  configuration = sqlc.arg(configuration)::jsonb, host = sqlc.arg(host), self_hosted = sqlc.arg(self_hosted),
  credential_ref = sqlc.narg(credential_ref), credential_set_at = sqlc.narg(credential_set_at), billing_mode = sqlc.arg(billing_mode),
  quota = sqlc.narg(quota)::jsonb, state = sqlc.arg(state), version = version + 1, changed_by = sqlc.arg(changed_by),
  changed_at = sqlc.arg(changed_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: InsertAIConnectionVersion :exec
INSERT INTO fyniq.ai_connection_version (connection_id, version, step, snapshot, approval_id, made_by, made_at)
VALUES (sqlc.arg(connection_id), sqlc.arg(version), sqlc.arg(step), sqlc.arg(snapshot)::jsonb, sqlc.narg(approval_id), sqlc.arg(made_by),
  sqlc.arg(made_at));

-- name: AIConnectionVersions :many
SELECT * FROM fyniq.ai_connection_version WHERE connection_id = sqlc.arg(connection_id) ORDER BY version;
