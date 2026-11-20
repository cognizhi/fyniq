-- name: InsertInvoiceSubmission :one
INSERT INTO fyniq.invoicenow_submission (document_id, document_uuid, version, kind, direction, recipient, payload_sha256, created_at, updated_at, created_by)
VALUES (sqlc.arg(document_id), sqlc.arg(document_uuid), sqlc.arg(version), sqlc.arg(kind), sqlc.arg(direction), sqlc.narg(recipient),
  sqlc.narg(payload_sha256), sqlc.arg(at), sqlc.arg(at), sqlc.arg(created_by))
RETURNING id, document_id, document_uuid, version, kind, direction, status, recipient, transmission_id, iras_ack, last_error, payload_sha256,
  created_at, updated_at, created_by;

-- name: InvoiceSubmissionOf :one
SELECT id, document_id, document_uuid, version, kind, direction, status, recipient, transmission_id, iras_ack, last_error, payload_sha256,
  created_at, updated_at, created_by
FROM fyniq.invoicenow_submission WHERE id = sqlc.arg(id);

-- name: InvoiceSubmissionLatestOfDocument :one
SELECT document_uuid, version FROM fyniq.invoicenow_submission WHERE document_id = sqlc.arg(document_id) ORDER BY version DESC LIMIT 1;

-- name: ListInvoiceSubmissions :many
SELECT id, document_id, document_uuid, version, kind, direction, status, recipient, transmission_id, iras_ack, last_error, payload_sha256,
  created_at, updated_at, created_by
FROM fyniq.invoicenow_submission
WHERE (sqlc.arg(status)::text = '' OR status = sqlc.arg(status))
  AND (sqlc.narg(document_id)::uuid IS NULL OR document_id = sqlc.narg(document_id))
  AND (sqlc.narg(after)::uuid IS NULL OR id < sqlc.narg(after))
ORDER BY id DESC LIMIT sqlc.arg(row_limit);

-- name: LatestInvoiceSubmissions :many
SELECT DISTINCT ON (document_id) id, document_id, document_uuid, version, kind, direction, status, recipient, transmission_id, iras_ack, last_error,
  payload_sha256, created_at, updated_at, created_by
FROM fyniq.invoicenow_submission WHERE document_id = ANY(sqlc.arg(documents)::uuid[])
ORDER BY document_id, version DESC;

-- name: MoveInvoiceSubmission :one
UPDATE fyniq.invoicenow_submission
SET status = sqlc.arg(status), transmission_id = coalesce(sqlc.narg(transmission_id), transmission_id), iras_ack = sqlc.narg(iras_ack),
  last_error = sqlc.narg(last_error), updated_at = sqlc.arg(at)
WHERE id = sqlc.arg(id)
RETURNING id, document_id, document_uuid, version, kind, direction, status, recipient, transmission_id, iras_ack, last_error, payload_sha256,
  created_at, updated_at, created_by;

-- name: InsertInvoiceEvent :exec
INSERT INTO fyniq.invoicenow_event (submission_id, at, status, detail, actor)
VALUES (sqlc.arg(submission_id), sqlc.arg(at), sqlc.arg(status), sqlc.narg(detail), sqlc.arg(actor));

-- name: InvoiceEventsOf :many
SELECT at, status, detail, actor FROM fyniq.invoicenow_event WHERE submission_id = sqlc.arg(submission_id) ORDER BY at, id;

-- name: InsertInvoiceExclusion :one
INSERT INTO fyniq.invoicenow_exclusion (document_id, reason, detail, recorded_by, recorded_at)
VALUES (sqlc.arg(document_id), sqlc.arg(reason), sqlc.narg(detail), sqlc.arg(recorded_by), sqlc.arg(at))
RETURNING id, document_id, reason, detail, recorded_by, recorded_at, withdrawn_at, withdrawn_by;

-- name: WithdrawInvoiceExclusion :one
UPDATE fyniq.invoicenow_exclusion SET withdrawn_at = sqlc.arg(at), withdrawn_by = sqlc.arg(by) WHERE id = sqlc.arg(id) AND withdrawn_at IS NULL
RETURNING id, document_id, reason, detail, recorded_by, recorded_at, withdrawn_at, withdrawn_by;

-- name: LiveInvoiceExclusions :many
SELECT id, document_id, reason, detail, recorded_by, recorded_at, withdrawn_at, withdrawn_by
FROM fyniq.invoicenow_exclusion WHERE withdrawn_at IS NULL AND document_id = ANY(sqlc.arg(documents)::uuid[]);

-- name: InsertInvoiceContingency :one
INSERT INTO fyniq.invoicenow_contingency (period_from, period_to, reason, evidence, requested_by, requested_at)
VALUES (sqlc.arg(period_from), sqlc.arg(period_to), sqlc.arg(reason), sqlc.arg(evidence), sqlc.arg(requested_by), sqlc.arg(at))
RETURNING id, period_from, period_to, reason, evidence, requested_by, requested_at, approved_by, approved_at, withdrawn_at;

-- name: ApproveInvoiceContingency :one
UPDATE fyniq.invoicenow_contingency SET approved_by = sqlc.arg(by), approved_at = sqlc.arg(at) WHERE id = sqlc.arg(id) AND approved_by IS NULL AND withdrawn_at IS NULL
RETURNING id, period_from, period_to, reason, evidence, requested_by, requested_at, approved_by, approved_at, withdrawn_at;

-- name: InvoiceContingencyOf :one
SELECT id, period_from, period_to, reason, evidence, requested_by, requested_at, approved_by, approved_at, withdrawn_at
FROM fyniq.invoicenow_contingency WHERE id = sqlc.arg(id);

-- name: InvoiceContingenciesCovering :many
SELECT id, period_from, period_to, reason, evidence, requested_by, requested_at, approved_by, approved_at, withdrawn_at
FROM fyniq.invoicenow_contingency WHERE withdrawn_at IS NULL AND period_from <= sqlc.arg(period_to) AND period_to >= sqlc.arg(period_from)
ORDER BY requested_at DESC, id DESC;

-- name: InvoiceExclusionExists :one
SELECT id FROM fyniq.invoicenow_exclusion WHERE id = sqlc.arg(id);
