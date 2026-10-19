-- name: ListGSTRegistrations :many
SELECT id, registered_from, deregistered_from, gst_number, return_frequency, quarter_start_month, accounting_basis, version, recorded_at FROM fyniq.gst_registration ORDER BY registered_from;

-- name: GetGSTRegistration :one
SELECT id, registered_from, deregistered_from, gst_number, return_frequency, quarter_start_month, accounting_basis, version, recorded_at FROM fyniq.gst_registration WHERE id = sqlc.arg(id);

-- name: LockGSTRegistration :one
SELECT id, registered_from, deregistered_from, gst_number, return_frequency, quarter_start_month, accounting_basis, version, recorded_at FROM fyniq.gst_registration WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: InsertGSTRegistration :one
INSERT INTO fyniq.gst_registration (registered_from, deregistered_from, gst_number, return_frequency, quarter_start_month,
  accounting_basis, recorded_at)
VALUES (sqlc.arg(registered_from), sqlc.narg(deregistered_from), sqlc.arg(gst_number), sqlc.arg(return_frequency),
  sqlc.arg(quarter_start_month), sqlc.arg(accounting_basis), sqlc.arg(recorded_at))
RETURNING id, registered_from, deregistered_from, gst_number, return_frequency, quarter_start_month, accounting_basis, version, recorded_at;

-- name: UpdateGSTRegistration :one
UPDATE fyniq.gst_registration SET registered_from = sqlc.arg(registered_from), deregistered_from = sqlc.narg(deregistered_from),
  gst_number = sqlc.arg(gst_number), return_frequency = sqlc.arg(return_frequency), quarter_start_month = sqlc.arg(quarter_start_month),
  accounting_basis = sqlc.arg(accounting_basis), recorded_at = sqlc.arg(recorded_at), version = version + 1
WHERE id = sqlc.arg(id)
RETURNING id, registered_from, deregistered_from, gst_number, return_frequency, quarter_start_month, accounting_basis, version, recorded_at;
