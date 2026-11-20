-- name: UpsertPSPCustomer :one
INSERT INTO fyniq.psp_customer (connector_id, provider_customer_id, name, email, seen_at)
VALUES (sqlc.arg(connector_id), sqlc.arg(provider_customer_id), sqlc.narg(name), sqlc.narg(email), sqlc.arg(seen_at))
ON CONFLICT (organisation_id, connector_id, provider_customer_id) DO UPDATE SET name = EXCLUDED.name, email = EXCLUDED.email, seen_at = EXCLUDED.seen_at
  WHERE (psp_customer.name, psp_customer.email) IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.email)
RETURNING id, connector_id, provider_customer_id, name, email, contact_id, seen_at, (xmax = 0) AS inserted;

-- name: GetPSPCustomer :one
SELECT id, connector_id, provider_customer_id, name, email, contact_id, seen_at FROM fyniq.psp_customer
WHERE connector_id = sqlc.arg(connector_id) AND provider_customer_id = sqlc.arg(provider_customer_id);

-- name: UpsertPSPSubscription :one
INSERT INTO fyniq.psp_subscription (connector_id, provider_subscription_id, provider_customer_id, status, currency, amount, interval_unit, interval_count,
  period_start, period_end, cancel_at_period_end, canceled_on, seen_at)
VALUES (sqlc.arg(connector_id), sqlc.arg(provider_subscription_id), sqlc.arg(provider_customer_id), sqlc.arg(status), sqlc.arg(currency), sqlc.arg(amount)::numeric,
  sqlc.arg(interval_unit), sqlc.arg(interval_count), sqlc.narg(period_start), sqlc.narg(period_end), sqlc.arg(cancel_at_period_end), sqlc.narg(canceled_on), sqlc.arg(seen_at))
ON CONFLICT (organisation_id, connector_id, provider_subscription_id) DO UPDATE SET provider_customer_id = EXCLUDED.provider_customer_id, status = EXCLUDED.status,
  currency = EXCLUDED.currency, amount = EXCLUDED.amount, interval_unit = EXCLUDED.interval_unit, interval_count = EXCLUDED.interval_count,
  period_start = EXCLUDED.period_start, period_end = EXCLUDED.period_end, cancel_at_period_end = EXCLUDED.cancel_at_period_end, canceled_on = EXCLUDED.canceled_on,
  seen_at = EXCLUDED.seen_at
  WHERE (psp_subscription.provider_customer_id, psp_subscription.status, psp_subscription.currency, psp_subscription.amount, psp_subscription.interval_unit,
    psp_subscription.interval_count, psp_subscription.period_start, psp_subscription.period_end, psp_subscription.cancel_at_period_end, psp_subscription.canceled_on)
    IS DISTINCT FROM (EXCLUDED.provider_customer_id, EXCLUDED.status, EXCLUDED.currency, EXCLUDED.amount, EXCLUDED.interval_unit, EXCLUDED.interval_count,
    EXCLUDED.period_start, EXCLUDED.period_end, EXCLUDED.cancel_at_period_end, EXCLUDED.canceled_on)
RETURNING id, connector_id, provider_subscription_id, provider_customer_id, status, currency, amount, interval_unit, interval_count, period_start, period_end,
  cancel_at_period_end, canceled_on, seen_at;

-- name: PSPSubscriptions :many
SELECT id, connector_id, provider_subscription_id, provider_customer_id, status, currency, amount, interval_unit, interval_count, period_start, period_end,
  cancel_at_period_end, canceled_on, seen_at FROM fyniq.psp_subscription ORDER BY provider_customer_id, provider_subscription_id;

-- name: InsertPSPInvoice :one
INSERT INTO fyniq.psp_invoice (connector_id, provider_invoice_id, provider_customer_id, provider_subscription_id, number, status, currency, total, amount_paid,
  attempt_count, next_attempt_on, outcome, hosted_url, finalized_on, due_on, draft_lines, seen_at)
VALUES (sqlc.arg(connector_id), sqlc.arg(provider_invoice_id), sqlc.arg(provider_customer_id), sqlc.narg(provider_subscription_id), sqlc.narg(number), sqlc.arg(status),
  sqlc.arg(currency), sqlc.arg(total)::numeric, sqlc.arg(amount_paid)::numeric, sqlc.arg(attempt_count), sqlc.narg(next_attempt_on), sqlc.narg(outcome), sqlc.narg(hosted_url),
  sqlc.narg(finalized_on), sqlc.narg(due_on), sqlc.arg(draft_lines)::jsonb, sqlc.arg(seen_at))
RETURNING id, connector_id, provider_invoice_id, provider_customer_id, provider_subscription_id, number, status, currency, total, amount_paid, attempt_count,
  next_attempt_on, outcome, hosted_url, finalized_on, due_on, draft_lines, state, parked_reason, document_id, version, seen_at;

-- name: GetPSPInvoice :one
SELECT id, connector_id, provider_invoice_id, provider_customer_id, provider_subscription_id, number, status, currency, total, amount_paid, attempt_count,
  next_attempt_on, outcome, hosted_url, finalized_on, due_on, draft_lines, state, parked_reason, document_id, version, seen_at
FROM fyniq.psp_invoice WHERE connector_id = sqlc.arg(connector_id) AND provider_invoice_id = sqlc.arg(provider_invoice_id);

-- name: PSPInvoicesInState :many
SELECT id, connector_id, provider_invoice_id, provider_customer_id, provider_subscription_id, number, status, currency, total, amount_paid, attempt_count,
  next_attempt_on, outcome, hosted_url, finalized_on, due_on, draft_lines, state, parked_reason, document_id, version, seen_at
FROM fyniq.psp_invoice WHERE (sqlc.arg(state)::text = '' OR state = sqlc.arg(state)::text) ORDER BY seen_at, id;

-- name: UpdatePSPInvoice :one
-- A mirrored invoice's collection state follows the provider; its lines and total only while it has no AR document.
UPDATE fyniq.psp_invoice SET provider_subscription_id = sqlc.narg(provider_subscription_id), number = sqlc.narg(number), status = sqlc.arg(status),
  amount_paid = sqlc.arg(amount_paid)::numeric, attempt_count = sqlc.arg(attempt_count), next_attempt_on = sqlc.narg(next_attempt_on), outcome = sqlc.narg(outcome),
  hosted_url = sqlc.narg(hosted_url), finalized_on = sqlc.narg(finalized_on), due_on = sqlc.narg(due_on),
  total = CASE WHEN document_id IS NULL THEN sqlc.arg(total)::numeric ELSE total END,
  draft_lines = CASE WHEN document_id IS NULL THEN sqlc.arg(draft_lines)::jsonb ELSE draft_lines END,
  seen_at = sqlc.arg(seen_at), version = version + 1
WHERE id = sqlc.arg(id)
RETURNING id, connector_id, provider_invoice_id, provider_customer_id, provider_subscription_id, number, status, currency, total, amount_paid, attempt_count,
  next_attempt_on, outcome, hosted_url, finalized_on, due_on, draft_lines, state, parked_reason, document_id, version, seen_at;

-- name: ParkPSPInvoice :exec
UPDATE fyniq.psp_invoice SET state = 'parked', parked_reason = sqlc.arg(parked_reason), version = version + 1 WHERE id = sqlc.arg(id) AND state IN ('mirrored', 'drafted');

-- name: PostedPSPInvoice :exec
UPDATE fyniq.psp_invoice SET state = 'posted', version = version + 1 WHERE id = sqlc.arg(id) AND state = 'drafted';

-- name: CreateProviderInvoiceDraft :one
SELECT d.document_id::uuid AS document_id, d.contact_id::uuid AS contact_id, d.contact_made::boolean AS contact_made
FROM fyniq.create_provider_invoice_draft(sqlc.arg(connector_id)::uuid, sqlc.arg(provider_invoice_id)::text) AS d;

-- name: PSPInvoiceDocuments :many
SELECT document_id::uuid AS document_id FROM fyniq.psp_invoice WHERE document_id IS NOT NULL;
