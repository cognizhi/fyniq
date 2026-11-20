-- name: InsertApprovalDelegation :one
INSERT INTO fyniq.approval_delegation (delegator, delegate, starts_on, ends_on, reason, created_by, created_at)
VALUES (sqlc.arg(delegator), sqlc.arg(delegate), sqlc.arg(starts_on), sqlc.arg(ends_on), sqlc.arg(reason), sqlc.arg(delegator), sqlc.arg(at))
RETURNING id, delegator, delegate, starts_on, ends_on, reason, status, ended_at, ended_by, version, created_at;

-- name: GetApprovalDelegation :one
SELECT id, delegator, delegate, starts_on, ends_on, reason, status, ended_at, ended_by, version, created_at
FROM fyniq.approval_delegation WHERE id = sqlc.arg(id);

-- name: LockApprovalDelegation :one
SELECT id, delegator, delegate, starts_on, ends_on, reason, status, ended_at, ended_by, version, created_at
FROM fyniq.approval_delegation WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: ListApprovalDelegations :many
SELECT id, delegator, delegate, starts_on, ends_on, reason, status, ended_at, ended_by, version, created_at
FROM fyniq.approval_delegation ORDER BY created_at, id;

-- name: EndApprovalDelegation :one
UPDATE fyniq.approval_delegation SET status = 'ended', ended_at = sqlc.arg(at), ended_by = sqlc.arg(by), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version) AND status = 'active'
RETURNING id, delegator, delegate, starts_on, ends_on, reason, status, ended_at, ended_by, version, created_at;

-- name: ApprovalRemindersSent :many
SELECT kind, number, sent_at FROM fyniq.approval_reminder WHERE request_id = sqlc.arg(request_id) ORDER BY sent_at, number;

-- name: InsertApprovalReminder :exec
INSERT INTO fyniq.approval_reminder (request_id, kind, number, sent_at)
VALUES (sqlc.arg(request_id), sqlc.arg(kind), sqlc.arg(number), sqlc.arg(at));
