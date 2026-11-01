-- name: InsertReportExport :one
INSERT INTO fyniq.report_export (report, format, parameters, requested_by, requested_at)
VALUES (sqlc.arg(report), sqlc.arg(format), sqlc.arg(parameters)::jsonb, sqlc.arg(requested_by), sqlc.arg(requested_at))
RETURNING *;

-- name: GetReportExport :one
SELECT * FROM fyniq.report_export WHERE id = sqlc.arg(id);

-- name: ReportExportsOf :many
SELECT * FROM fyniq.report_export WHERE requested_by = sqlc.arg(requested_by) ORDER BY requested_at DESC, id DESC LIMIT sqlc.arg(page_size);

-- name: ReadyReportExport :one
UPDATE fyniq.report_export SET status = 'ready', ledger_revision = sqlc.arg(ledger_revision), file_name = sqlc.arg(file_name),
  media_type = sqlc.arg(media_type), size_bytes = sqlc.arg(size_bytes), file_sha256 = sqlc.arg(file_sha256), rows_count = sqlc.arg(rows_count),
  ready_at = sqlc.arg(ready_at), expires_at = sqlc.arg(expires_at)
WHERE id = sqlc.arg(id) RETURNING *;

-- name: FailReportExport :one
UPDATE fyniq.report_export SET status = 'failed', failure = sqlc.arg(failure) WHERE id = sqlc.arg(id) RETURNING *;

-- name: ExpiringReportExports :many
SELECT * FROM fyniq.report_export WHERE status = 'ready' AND expires_at <= sqlc.arg(now) ORDER BY expires_at, id LIMIT sqlc.arg(page_size);

-- name: ExpireReportExport :exec
UPDATE fyniq.report_export SET status = 'expired' WHERE id = sqlc.arg(id);
