-- name: AIConnections :many
SELECT * FROM fyniq.ai_connection ORDER BY name, id;

-- name: GetAIConnection :one
SELECT * FROM fyniq.ai_connection WHERE id = sqlc.arg(id);

-- name: GetAIConnectionForUpdate :one
SELECT * FROM fyniq.ai_connection WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: InsertAIConnection :one
INSERT INTO fyniq.ai_connection (id, name, connector, preset, auth, configuration, host, self_hosted, credential_ref, credential_set_at,
  billing_mode, quota, data_handling, auto_qualify, created_by, created_at, changed_by, changed_at)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(connector), sqlc.narg(preset), sqlc.arg(auth), sqlc.arg(configuration)::jsonb, sqlc.arg(host),
  sqlc.arg(self_hosted), sqlc.narg(credential_ref), sqlc.narg(credential_set_at), sqlc.arg(billing_mode), sqlc.narg(quota)::jsonb,
  sqlc.arg(data_handling)::jsonb, sqlc.arg(auto_qualify), sqlc.arg(created_by), sqlc.arg(created_at), sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING *;

-- name: UpdateAIConnection :one
UPDATE fyniq.ai_connection SET name = sqlc.arg(name), preset = sqlc.narg(preset), auth = sqlc.arg(auth),
  configuration = sqlc.arg(configuration)::jsonb, host = sqlc.arg(host), self_hosted = sqlc.arg(self_hosted),
  credential_ref = sqlc.narg(credential_ref), credential_set_at = sqlc.narg(credential_set_at), billing_mode = sqlc.arg(billing_mode),
  quota = sqlc.narg(quota)::jsonb, data_handling = sqlc.arg(data_handling)::jsonb, auto_qualify = sqlc.arg(auto_qualify), state = sqlc.arg(state), version = version + 1, changed_by = sqlc.arg(changed_by),
  changed_at = sqlc.arg(changed_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: InsertAIConnectionVersion :exec
INSERT INTO fyniq.ai_connection_version (connection_id, version, step, snapshot, approval_id, made_by, made_at)
VALUES (sqlc.arg(connection_id), sqlc.arg(version), sqlc.arg(step), sqlc.arg(snapshot)::jsonb, sqlc.narg(approval_id), sqlc.arg(made_by),
  sqlc.arg(made_at));

-- name: AIConnectionVersions :many
SELECT * FROM fyniq.ai_connection_version WHERE connection_id = sqlc.arg(connection_id) ORDER BY version;

-- name: LatestAIDataPolicy :one
SELECT * FROM fyniq.ai_data_policy ORDER BY version DESC LIMIT 1;

-- name: InsertAIDataPolicy :one
INSERT INTO fyniq.ai_data_policy (version, policy, approval_id, made_by, made_at)
VALUES (sqlc.arg(version), sqlc.arg(policy)::jsonb, sqlc.arg(approval_id), sqlc.arg(made_by), sqlc.arg(made_at))
RETURNING *;

-- name: AIModelsOf :many
SELECT * FROM fyniq.ai_model WHERE connection_id = sqlc.arg(connection_id) ORDER BY model;

-- name: UpsertAIModel :exec
INSERT INTO fyniq.ai_model (connection_id, model, name, alias_of, provider_created, shutdown_at, context_window, max_output, reports,
  first_listed_at, last_listed_at, listed, not_callable_at, allowed)
VALUES (sqlc.arg(connection_id), sqlc.arg(model), sqlc.narg(name), sqlc.narg(alias_of), sqlc.narg(provider_created), sqlc.narg(shutdown_at),
  sqlc.narg(context_window), sqlc.narg(max_output), sqlc.arg(reports)::text[], sqlc.arg(first_listed_at), sqlc.arg(last_listed_at), sqlc.arg(listed),
  sqlc.narg(not_callable_at), sqlc.arg(allowed))
ON CONFLICT (organisation_id, connection_id, model) DO UPDATE SET name = EXCLUDED.name, alias_of = EXCLUDED.alias_of,
  provider_created = EXCLUDED.provider_created, shutdown_at = EXCLUDED.shutdown_at, context_window = EXCLUDED.context_window,
  max_output = EXCLUDED.max_output, reports = EXCLUDED.reports, last_listed_at = EXCLUDED.last_listed_at, listed = EXCLUDED.listed,
  not_callable_at = EXCLUDED.not_callable_at, allowed = fyniq.ai_model.allowed AND EXCLUDED.allowed;

-- name: InsertAIModelListRead :one
INSERT INTO fyniq.ai_model_list_read (connection_id, trigger, outcome, pages, added, removed, failure, read_by, read_at)
VALUES (sqlc.arg(connection_id), sqlc.arg(trigger), sqlc.arg(outcome), sqlc.arg(pages), sqlc.arg(added), sqlc.arg(removed), sqlc.narg(failure),
  sqlc.arg(read_by), sqlc.arg(read_at))
RETURNING *;

-- name: AIModelListReads :many
SELECT * FROM fyniq.ai_model_list_read WHERE connection_id = sqlc.arg(connection_id) ORDER BY read_at DESC, id DESC LIMIT sqlc.arg(page_size);

-- name: LastCompleteAIModelListRead :one
SELECT * FROM fyniq.ai_model_list_read WHERE connection_id = sqlc.arg(connection_id) AND outcome = 'complete' ORDER BY read_at DESC, id DESC LIMIT 1;

-- name: ProbeAIModel :one
UPDATE fyniq.ai_model SET probed_at = sqlc.arg(probed_at), probe_ok = sqlc.arg(probe_ok), probe_detail = sqlc.narg(probe_detail),
  not_callable_at = sqlc.narg(not_callable_at), allowed = allowed AND sqlc.arg(probe_ok)
WHERE connection_id = sqlc.arg(connection_id) AND model = sqlc.arg(model)
RETURNING *;

-- name: AllowAIModel :one
UPDATE fyniq.ai_model SET allowed = sqlc.arg(allowed) WHERE connection_id = sqlc.arg(connection_id) AND model = sqlc.arg(model)
RETURNING *;
