-- name: InsertPaymentLink :one
INSERT INTO fyniq.payment_link (connector_id, invoice_id, provider_request_id, url, reference, currency, amount, expires_at, created_by, created_at)
VALUES (sqlc.arg(connector_id), sqlc.arg(invoice_id), sqlc.arg(provider_request_id), sqlc.arg(url), sqlc.arg(reference), sqlc.arg(currency),
  sqlc.arg(amount)::numeric, sqlc.arg(expires_at), sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING id, connector_id, invoice_id, provider_request_id, url, reference, currency, amount, status, closed_reason, expires_at, version, created_by, created_at, closed_at;

-- name: GetPaymentLink :one
SELECT id, connector_id, invoice_id, provider_request_id, url, reference, currency, amount, status, closed_reason, expires_at, version, created_by, created_at, closed_at
  FROM fyniq.payment_link WHERE id = sqlc.arg(id);

-- name: PaymentLinksOfInvoice :many
SELECT id, connector_id, invoice_id, provider_request_id, url, reference, currency, amount, status, closed_reason, expires_at, version, created_by, created_at, closed_at
  FROM fyniq.payment_link WHERE invoice_id = sqlc.arg(invoice_id) ORDER BY created_at DESC, id DESC;

-- name: PaymentLinkByRequest :one
SELECT id, connector_id, invoice_id, provider_request_id, url, reference, currency, amount, status, closed_reason, expires_at, version, created_by, created_at, closed_at
  FROM fyniq.payment_link WHERE connector_id = sqlc.arg(connector_id) AND provider_request_id = sqlc.arg(provider_request_id);

-- name: ClosePaymentLink :one
UPDATE fyniq.payment_link SET status = sqlc.arg(status), closed_reason = sqlc.arg(closed_reason), closed_at = sqlc.arg(closed_at), version = version + 1
 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING id, connector_id, invoice_id, provider_request_id, url, reference, currency, amount, status, closed_reason, expires_at, version, created_by, created_at, closed_at;

-- name: InsertPaymentLinkPayment :one
INSERT INTO fyniq.payment_link_payment (link_id, connector_id, provider_payment_id, currency, amount, paid_at, seen_at)
VALUES (sqlc.arg(link_id), sqlc.arg(connector_id), sqlc.arg(provider_payment_id), sqlc.arg(currency), sqlc.arg(amount)::numeric, sqlc.arg(paid_at), sqlc.arg(seen_at))
ON CONFLICT (organisation_id, connector_id, provider_payment_id) DO NOTHING
RETURNING id, link_id, connector_id, provider_payment_id, currency, amount, paid_at, seen_at;

-- name: PaymentLinkPaymentByCharge :one
SELECT id, link_id, connector_id, provider_payment_id, currency, amount, paid_at, seen_at
  FROM fyniq.payment_link_payment WHERE connector_id = sqlc.arg(connector_id) AND provider_payment_id = sqlc.arg(provider_payment_id);

-- name: PaymentLinkPaymentsOf :many
SELECT id, link_id, connector_id, provider_payment_id, currency, amount, paid_at, seen_at
  FROM fyniq.payment_link_payment WHERE link_id = sqlc.arg(link_id) ORDER BY paid_at, id;
