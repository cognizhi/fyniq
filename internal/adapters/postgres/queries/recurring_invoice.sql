-- name: InsertRecurringInvoice :one
INSERT INTO fyniq.recurring_invoice (name, status, every_months, every_weeks, day_of_month, starts_on, ends_on, next_on)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, version, content_version;

-- name: InsertRecurringInvoiceVersion :exec
INSERT INTO fyniq.recurring_invoice_version (recurring_invoice_id, version, content, edited_by) VALUES ($1, $2, $3, $4);

-- name: GetRecurringInvoice :one
SELECT r.id, r.name, r.status, r.every_months, r.every_weeks, r.day_of_month, r.starts_on, r.ends_on, r.next_on, r.version,
       r.content_version, r.last_on, r.last_invoice_id, v.content, v.edited_by, v.edited_at
FROM fyniq.recurring_invoice r
JOIN fyniq.recurring_invoice_version v
  ON v.organisation_id = r.organisation_id AND v.recurring_invoice_id = r.id AND v.version = r.content_version
WHERE r.id = $1;

-- name: LockRecurringInvoice :one
-- The recurring invoice as it stands, locked for its change.
SELECT r.id, r.name, r.status, r.every_months, r.every_weeks, r.day_of_month, r.starts_on, r.ends_on, r.next_on, r.version,
       r.content_version, r.last_on, r.last_invoice_id, v.content, v.edited_by, v.edited_at
FROM fyniq.recurring_invoice r
JOIN fyniq.recurring_invoice_version v
  ON v.organisation_id = r.organisation_id AND v.recurring_invoice_id = r.id AND v.version = r.content_version
WHERE r.id = $1
FOR UPDATE OF r;

-- name: UpdateRecurringInvoice :exec
UPDATE fyniq.recurring_invoice
SET name = $2, status = $3, every_months = $4, every_weeks = $5, day_of_month = $6, starts_on = $7, ends_on = $8, next_on = $9,
    version = $10, content_version = $11
WHERE id = $1;

-- name: PageRecurringInvoices :many
-- By name, then ID, after the cursor's (name, id).
SELECT r.id, r.name, r.status, r.every_months, r.every_weeks, r.day_of_month, r.starts_on, r.ends_on, r.next_on, r.version,
       r.content_version, r.last_on, r.last_invoice_id, v.content, v.edited_by, v.edited_at
FROM fyniq.recurring_invoice r
JOIN fyniq.recurring_invoice_version v
  ON v.organisation_id = r.organisation_id AND v.recurring_invoice_id = r.id AND v.version = r.content_version
WHERE sqlc.narg(after_id)::uuid IS NULL OR (r.name, r.id) > (sqlc.arg(after_name)::text, sqlc.narg(after_id)::uuid)
ORDER BY r.name, r.id
LIMIT sqlc.arg(page_size);

-- name: GetRecurringInvoiceVersion :one
SELECT version, content, edited_by, edited_at FROM fyniq.recurring_invoice_version
WHERE recurring_invoice_id = $1 AND version = $2;

-- name: DueRecurringInvoices :many
-- Active recurring invoices whose next occurrence is on or before a day, earliest first (FR-AR-07, FR-AUT-01).
SELECT id FROM fyniq.recurring_invoice
WHERE status = 'active' AND next_on IS NOT NULL AND next_on <= sqlc.arg(on_day)::date
ORDER BY next_on, id
LIMIT sqlc.arg(max_rows);

-- name: CreateScheduledInvoiceDraft :one
-- The scheduler's one way to make a bill: the draft of a recurring invoice's next occurrence (FY199).
SELECT fyniq.create_scheduled_invoice_draft(sqlc.arg(recurring_invoice_id)::uuid, sqlc.arg(on_day)::date)::uuid AS id;

-- name: SetRecurringInvoiceNext :exec
-- The worker moves the next occurrence once its draft is made, and changes nothing else.
UPDATE fyniq.recurring_invoice SET next_on = sqlc.narg(next_on)::date WHERE id = sqlc.arg(id);
