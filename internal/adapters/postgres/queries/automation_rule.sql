-- name: InsertAutomationRule :one
INSERT INTO fyniq.automation_rule (name, trigger, document_type, amount_at_least, action, recipients, created_by, created_at, updated_at)
VALUES (sqlc.arg(name), 'document.posted', sqlc.narg(document_type), sqlc.narg(amount_at_least)::numeric, 'notify', sqlc.arg(recipients)::uuid[], sqlc.arg(by), sqlc.arg(at), sqlc.arg(at))
RETURNING id, name, trigger, document_type, amount_at_least, action, recipients, state, version, created_by, created_at, updated_at;

-- name: GetAutomationRule :one
SELECT id, name, trigger, document_type, amount_at_least, action, recipients, state, version, created_by, created_at, updated_at
FROM fyniq.automation_rule WHERE id = sqlc.arg(id);

-- name: LockAutomationRule :one
SELECT id, name, trigger, document_type, amount_at_least, action, recipients, state, version, created_by, created_at, updated_at
FROM fyniq.automation_rule WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: ListAutomationRules :many
SELECT id, name, trigger, document_type, amount_at_least, action, recipients, state, version, created_by, created_at, updated_at
FROM fyniq.automation_rule ORDER BY created_at, id;

-- name: ActiveAutomationRules :many
SELECT id, name, trigger, document_type, amount_at_least, action, recipients, state, version, created_by, created_at, updated_at
FROM fyniq.automation_rule WHERE state = 'active' AND trigger = sqlc.arg(trigger) ORDER BY created_at, id;

-- name: ChangeAutomationRule :one
UPDATE fyniq.automation_rule SET name = sqlc.arg(name), document_type = sqlc.narg(document_type), amount_at_least = sqlc.narg(amount_at_least)::numeric,
  recipients = sqlc.arg(recipients)::uuid[], state = sqlc.arg(state), version = version + 1, updated_at = sqlc.arg(at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING id, name, trigger, document_type, amount_at_least, action, recipients, state, version, created_by, created_at, updated_at;
