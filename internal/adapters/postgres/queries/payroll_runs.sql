-- name: LatestPayrollMapping :one
SELECT id, version, set_by, set_at FROM fyniq.payroll_mapping ORDER BY version DESC LIMIT 1;

-- name: PayrollMappingEntries :many
SELECT key, account_id FROM fyniq.payroll_mapping_entry WHERE mapping_id = sqlc.arg(mapping_id) ORDER BY key;

-- name: InsertPayrollMapping :one
INSERT INTO fyniq.payroll_mapping (version, set_by, set_at) VALUES (sqlc.arg(version), sqlc.arg(set_by), sqlc.arg(set_at)) RETURNING id, version, set_by, set_at;

-- name: InsertPayrollMappingEntry :exec
INSERT INTO fyniq.payroll_mapping_entry (mapping_id, key, account_id) VALUES (sqlc.arg(mapping_id), sqlc.arg(key), sqlc.arg(account_id));

-- name: LatestPayrollReceipt :one
SELECT id, link_id, epoch, source_system, run_id, version, wage_month, envelope_sha256, approver_issuer, approver_subject, preparer_issuer, preparer_subject,
       mapping_version, entry_id, totals, received_at
FROM fyniq.payroll_run_receipt WHERE source_system = sqlc.arg(source_system) AND run_id = sqlc.arg(run_id) ORDER BY version DESC LIMIT 1;

-- name: PayrollEnvelopeUsed :one
SELECT EXISTS (SELECT 1 FROM fyniq.payroll_run_receipt WHERE envelope_sha256 = sqlc.arg(envelope_sha256)) AS used;

-- name: InsertPayrollReceipt :one
INSERT INTO fyniq.payroll_run_receipt (link_id, epoch, source_system, run_id, version, wage_month, envelope_sha256, approver_issuer, approver_subject, preparer_issuer,
                                       preparer_subject, mapping_version, entry_id, totals, received_at)
VALUES (sqlc.arg(link_id), sqlc.arg(epoch), sqlc.arg(source_system), sqlc.arg(run_id), sqlc.arg(version), sqlc.arg(wage_month), sqlc.arg(envelope_sha256),
        sqlc.arg(approver_issuer), sqlc.arg(approver_subject), sqlc.arg(preparer_issuer), sqlc.arg(preparer_subject), sqlc.arg(mapping_version), sqlc.arg(entry_id),
        sqlc.arg(totals), sqlc.arg(received_at))
RETURNING id;

-- name: PayrollReceiptMonths :many
SELECT DISTINCT wage_month FROM fyniq.payroll_run_receipt ORDER BY wage_month;

-- name: InsertPayrollRunRefusal :one
INSERT INTO fyniq.payroll_run_refusal (link_id, run_id, version, code, refused_at)
VALUES (sqlc.arg(link_id), sqlc.arg(run_id), sqlc.arg(version), sqlc.arg(code), sqlc.arg(refused_at)) RETURNING id;

-- name: OpenPayrollRunRefusals :many
SELECT DISTINCT ON (r.run_id) r.run_id, r.version, r.code, r.refused_at FROM fyniq.payroll_run_refusal r
WHERE NOT EXISTS (SELECT 1 FROM fyniq.payroll_run_receipt p WHERE p.run_id = r.run_id AND p.version >= r.version)
ORDER BY r.run_id, r.version DESC, r.refused_at DESC;

-- name: PayrollReceiptsOfMonth :many
SELECT id, link_id, epoch, source_system, run_id, version, wage_month, envelope_sha256, approver_issuer, approver_subject, preparer_issuer, preparer_subject,
       mapping_version, entry_id, totals, received_at
FROM fyniq.payroll_run_receipt WHERE wage_month = sqlc.arg(wage_month) ORDER BY run_id, version;

-- name: PayrollRefusalsOfRun :many
SELECT run_id, version, code, refused_at FROM fyniq.payroll_run_refusal WHERE run_id = ANY(sqlc.arg(runs)::uuid[]) ORDER BY run_id, version, refused_at;
