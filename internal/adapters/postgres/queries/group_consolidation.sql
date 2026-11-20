-- name: GetGroupSettings :one
SELECT pairs::text AS pairs, version FROM fyniq.group_settings;

-- name: InsertGroupSettings :exec
INSERT INTO fyniq.group_settings (pairs, updated_by, updated_at) VALUES (sqlc.arg(pairs)::jsonb, sqlc.arg(by), sqlc.arg(at));

-- name: UpdateGroupSettings :one
UPDATE fyniq.group_settings SET pairs = sqlc.arg(pairs)::jsonb, version = version + 1, updated_by = sqlc.arg(by), updated_at = sqlc.arg(at) WHERE version = sqlc.arg(version)
RETURNING version;

-- name: ListGroupMappings :many
SELECT id, entity_row, entity_account_code, group_account_id, counterparty_row, counterparty_parent, version, updated_at
FROM fyniq.group_account_map ORDER BY entity_row NULLS FIRST, entity_account_code;

-- name: GetGroupMappingByCode :one
SELECT id, entity_row, entity_account_code, group_account_id, counterparty_row, counterparty_parent, version, updated_at
FROM fyniq.group_account_map WHERE entity_row IS NOT DISTINCT FROM sqlc.narg(entity_row)::uuid AND entity_account_code = sqlc.arg(code) FOR UPDATE;

-- name: InsertGroupMapping :one
INSERT INTO fyniq.group_account_map (entity_row, entity_account_code, group_account_id, counterparty_row, counterparty_parent, created_by, created_at, updated_by, updated_at)
VALUES (sqlc.narg(entity_row), sqlc.arg(code), sqlc.arg(group_account_id), sqlc.narg(counterparty_row), sqlc.arg(counterparty_parent), sqlc.arg(by), sqlc.arg(at), sqlc.arg(by), sqlc.arg(at))
RETURNING id, entity_row, entity_account_code, group_account_id, counterparty_row, counterparty_parent, version, updated_at;

-- name: UpdateGroupMapping :one
UPDATE fyniq.group_account_map SET group_account_id = sqlc.arg(group_account_id), counterparty_row = sqlc.narg(counterparty_row), counterparty_parent = sqlc.arg(counterparty_parent),
  version = version + 1, updated_by = sqlc.arg(by), updated_at = sqlc.arg(at)
WHERE id = sqlc.arg(id)
RETURNING id, entity_row, entity_account_code, group_account_id, counterparty_row, counterparty_parent, version, updated_at;

-- name: ListConsolidationRates :many
SELECT id, currency, month, closing, average, note, entered_by, entered_at FROM fyniq.consolidation_rate ORDER BY month DESC, currency, entered_at DESC, id;

-- name: InsertConsolidationRate :one
INSERT INTO fyniq.consolidation_rate (currency, month, closing, average, note, entered_by, entered_at)
VALUES (sqlc.arg(currency), sqlc.arg(month), sqlc.arg(closing)::numeric, sqlc.arg(average)::numeric, sqlc.arg(note), sqlc.arg(by), sqlc.arg(at))
RETURNING id, currency, month, closing, average, note, entered_by, entered_at;

-- name: InsertConsolidationRun :one
INSERT INTO fyniq.consolidation_run (from_month, to_month, currency, provisional, inputs, result, created_by, created_at)
VALUES (sqlc.arg(from_month), sqlc.arg(to_month), sqlc.arg(currency), sqlc.arg(provisional), sqlc.arg(inputs)::jsonb, sqlc.arg(result)::jsonb, sqlc.arg(by), sqlc.arg(at))
RETURNING id, from_month, to_month, currency, provisional, inputs::text AS inputs, result::text AS result, created_by, created_at;

-- name: GetConsolidationRun :one
SELECT id, from_month, to_month, currency, provisional, inputs::text AS inputs, result::text AS result, created_by, created_at FROM fyniq.consolidation_run WHERE id = sqlc.arg(id);

-- name: ListConsolidationRuns :many
SELECT id, from_month, to_month, currency, provisional, created_by, created_at FROM fyniq.consolidation_run ORDER BY created_at DESC, id;
