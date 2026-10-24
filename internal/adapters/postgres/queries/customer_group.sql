-- name: InsertCustomerGroup :one
INSERT INTO fyniq.customer_group (name, reminder_days) VALUES (sqlc.arg(name), sqlc.arg(reminder_days)::integer[])
RETURNING id, name, reminder_days, active, version;

-- name: GetCustomerGroup :one
SELECT id, name, reminder_days, active, version FROM fyniq.customer_group WHERE id = sqlc.arg(id);

-- name: LockCustomerGroup :one
SELECT id, name, reminder_days, active, version FROM fyniq.customer_group WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: ListCustomerGroups :many
SELECT id, name, reminder_days, active, version FROM fyniq.customer_group ORDER BY lower(name), id;

-- name: UpdateCustomerGroup :one
UPDATE fyniq.customer_group SET name = sqlc.arg(name), reminder_days = sqlc.arg(reminder_days)::integer[], active = sqlc.arg(active),
  version = version + 1
WHERE id = sqlc.arg(id)
RETURNING id, name, reminder_days, active, version;

-- name: InsertSalesReminder :one
INSERT INTO fyniq.sales_reminder (document_id, step, outcome, send_id, reason, decided_by)
VALUES (sqlc.arg(document_id), sqlc.arg(step), sqlc.arg(outcome), sqlc.narg(send_id), sqlc.narg(reason), sqlc.arg(decided_by))
RETURNING id, document_id, step, outcome, send_id, reason, decided_by, decided_at;

-- name: SalesRemindersOf :many
SELECT id, document_id, step, outcome, send_id, reason, decided_by, decided_at FROM fyniq.sales_reminder
WHERE document_id = ANY(sqlc.arg(documents)::uuid[])
ORDER BY document_id, step;
