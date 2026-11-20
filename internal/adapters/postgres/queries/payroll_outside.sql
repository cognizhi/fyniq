-- name: InsertPayrollArrangement :one
INSERT INTO fyniq.payroll_arrangement (item, method, provider, note, evidence_id, effective_from, recorded_by, recorded_at)
VALUES (sqlc.arg(item), sqlc.arg(method), sqlc.narg(provider), sqlc.arg(note), sqlc.narg(evidence_id), sqlc.arg(effective_from), sqlc.arg(recorded_by), sqlc.arg(recorded_at))
RETURNING *;

-- name: PayrollArrangements :many
-- The arrangement of each item in force for a month: the latest row that holds from a month no later than it.
SELECT DISTINCT ON (item) * FROM fyniq.payroll_arrangement WHERE effective_from <= sqlc.arg(month)::date ORDER BY item, effective_from DESC, recorded_at DESC, id DESC;

-- name: PayrollArrangementHistory :many
SELECT * FROM fyniq.payroll_arrangement ORDER BY recorded_at DESC, id DESC;

-- name: InsertPayrollImport :one
INSERT INTO fyniq.payroll_import (source_system, period, version, file_sha256, journal_id, line_count, totals, created_by, created_at)
VALUES (sqlc.arg(source_system), sqlc.arg(period), sqlc.arg(version), sqlc.arg(file_sha256), sqlc.arg(journal_id), sqlc.arg(line_count), sqlc.arg(totals), sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING *;

-- name: LatestPayrollImport :one
SELECT * FROM fyniq.payroll_import WHERE source_system = sqlc.arg(source_system) AND period = sqlc.arg(period) ORDER BY version DESC LIMIT 1;

-- name: PayrollImportVersion :one
SELECT * FROM fyniq.payroll_import WHERE source_system = sqlc.arg(source_system) AND period = sqlc.arg(period) AND version = sqlc.arg(version);

-- name: PayrollImports :many
SELECT * FROM fyniq.payroll_import ORDER BY period DESC, version DESC, created_at DESC;
