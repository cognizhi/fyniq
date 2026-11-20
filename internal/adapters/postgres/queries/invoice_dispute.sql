-- name: InsertInvoiceDispute :one
INSERT INTO fyniq.invoice_dispute (invoice_id, kind, amount, currency, reason, promised_on, created_by, created_at)
VALUES (sqlc.arg(invoice_id), sqlc.arg(kind), sqlc.narg(amount)::numeric, sqlc.narg(currency), sqlc.narg(reason), sqlc.narg(promised_on), sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING id, invoice_id, kind, amount, currency, reason, promised_on, status, resolution, version, created_by, created_at, resolved_by, resolved_at;

-- name: GetInvoiceDispute :one
SELECT id, invoice_id, kind, amount, currency, reason, promised_on, status, resolution, version, created_by, created_at, resolved_by, resolved_at
FROM fyniq.invoice_dispute WHERE id = sqlc.arg(id);

-- name: InvoiceDisputesOfInvoice :many
SELECT id, invoice_id, kind, amount, currency, reason, promised_on, status, resolution, version, created_by, created_at, resolved_by, resolved_at
FROM fyniq.invoice_dispute WHERE invoice_id = sqlc.arg(invoice_id) ORDER BY created_at DESC, id DESC;

-- name: OpenInvoiceDisputes :many
SELECT id, invoice_id, kind, amount, currency, reason, promised_on, status, resolution, version, created_by, created_at, resolved_by, resolved_at
FROM fyniq.invoice_dispute WHERE status = 'open' ORDER BY created_at, id;

-- name: ResolveInvoiceDispute :one
UPDATE fyniq.invoice_dispute SET status = 'resolved', resolution = sqlc.arg(resolution), resolved_by = sqlc.arg(resolved_by), resolved_at = sqlc.arg(resolved_at), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING id, invoice_id, kind, amount, currency, reason, promised_on, status, resolution, version, created_by, created_at, resolved_by, resolved_at;
