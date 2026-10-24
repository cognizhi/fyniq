-- name: InsertAllocation :one
-- The amounts' minor units come from the currencies, as a journal line's do; an unknown currency inserts nothing.
INSERT INTO fyniq.allocation (source_id, source_version, document_id, on_date, currency, minor_units, amount, base_currency,
  base_minor_units, base_amount, realised_fx, tolerance, reverses_id, written_by, written_at)
SELECT sqlc.arg(source_id)::uuid, sqlc.arg(source_version)::integer, sqlc.arg(document_id)::uuid, sqlc.arg(on_date)::date,
  c.code, c.minor_units, sqlc.arg(amount)::numeric, b.code, b.minor_units, sqlc.arg(base_amount)::numeric,
  sqlc.arg(realised_fx)::numeric, sqlc.arg(tolerance)::numeric, sqlc.narg(reverses_id)::uuid, sqlc.arg(written_by)::text,
  sqlc.arg(written_at)::timestamptz
FROM fyniq.currencies c, fyniq.currencies b
WHERE c.code = sqlc.arg(currency)::text AND b.code = sqlc.arg(base_currency)::text
RETURNING id, source_id, source_version, document_id, on_date, currency, amount, base_currency, base_amount, realised_fx,
  tolerance, reverses_id, written_by, written_at;

-- name: AllocationsOfDocument :many
-- What has settled a document: every allocation and reversing allocation, earliest first.
SELECT id, source_id, source_version, document_id, on_date, currency, amount, base_currency, base_amount, realised_fx,
  tolerance, reverses_id, written_by, written_at
FROM fyniq.allocation
WHERE document_id = sqlc.arg(document_id)
ORDER BY on_date, written_at, id;

-- name: AllocationsOfSource :many
-- What a settling document allocated: every allocation and reversing allocation, earliest first.
SELECT id, source_id, source_version, document_id, on_date, currency, amount, base_currency, base_amount, realised_fx,
  tolerance, reverses_id, written_by, written_at
FROM fyniq.allocation
WHERE source_id = sqlc.arg(source_id)
ORDER BY on_date, written_at, id;

-- name: SettledOfDocuments :many
-- The amount settled of each of the documents named, in their currencies: allocations less their reversals.
SELECT document_id, coalesce(sum(amount), 0)::numeric(20, 4) AS settled
FROM fyniq.allocation
WHERE document_id = ANY (sqlc.arg(document_ids)::uuid[])
GROUP BY document_id;

-- name: ToleranceBetween :one
-- The carrying amounts settlement tolerances cleared on allocations dated from from_day to to_day, net of reversals
-- (FR-BNK-08's cap per month).
SELECT coalesce(sum(tolerance), 0)::numeric(20, 4) AS tolerance
FROM fyniq.allocation
WHERE on_date BETWEEN sqlc.arg(from_day)::date AND sqlc.arg(to_day)::date;
