-- name: InsertPaymentBatchRelease :one
-- The total's minor units come from its currency; an unknown currency inserts nothing.
INSERT INTO fyniq.payment_batch_release (batch_id, batch_version, approval_id, released_by, released_at, writer, file_name, media_type,
                                         file_sealed, file_sha256, payments, currency, minor_units, total)
SELECT sqlc.arg(batch_id)::uuid, sqlc.arg(batch_version)::integer, sqlc.arg(approval_id)::uuid, sqlc.arg(released_by)::uuid,
       sqlc.arg(released_at)::timestamptz, sqlc.arg(writer)::text, sqlc.arg(file_name)::text, sqlc.arg(media_type)::text,
       sqlc.arg(file_sealed)::bytea, sqlc.arg(file_sha256)::bytea, sqlc.arg(payments)::integer, c.code, c.minor_units, sqlc.arg(total)::numeric
FROM fyniq.currencies c WHERE c.code = sqlc.arg(currency)::text
RETURNING *;

-- name: PaymentBatchReleaseOf :many
SELECT * FROM fyniq.payment_batch_release WHERE batch_id = sqlc.arg(batch_id);

-- name: InsertPaymentBatchPayment :exec
INSERT INTO fyniq.payment_batch_payment (release_id, payment_id) VALUES (sqlc.arg(release_id), sqlc.arg(payment_id));

-- name: PaymentBatchPayments :many
SELECT payment_id FROM fyniq.payment_batch_payment WHERE release_id = sqlc.arg(release_id) ORDER BY id;

-- name: PaymentBatchReleaseOfPayment :many
SELECT r.* FROM fyniq.payment_batch_release r
JOIN fyniq.payment_batch_payment p ON p.organisation_id = r.organisation_id AND p.release_id = r.id
WHERE p.payment_id = sqlc.arg(payment_id);
