-- name: InsertApprovalRequest :one
INSERT INTO fyniq.approval_request (object_type, object_key, operation, summary, change, envelope, envelope_hash, policy_version,
  policy_steps, requested_by, requested_at, requester_mfa_method, requester_mfa_at, expires_at)
VALUES (sqlc.arg(object_type), sqlc.arg(object_key), sqlc.arg(operation), sqlc.arg(summary), sqlc.arg(change)::jsonb, sqlc.arg(envelope),
  sqlc.arg(envelope_hash), sqlc.arg(policy_version), sqlc.arg(policy_steps)::jsonb, sqlc.arg(requested_by), sqlc.arg(requested_at),
  sqlc.arg(requester_mfa_method), sqlc.arg(requester_mfa_at), sqlc.arg(expires_at))
RETURNING id, object_type, object_key, operation, summary, change, envelope, envelope_hash, policy_version, policy_steps, requested_by,
  requested_at, requester_mfa_method, requester_mfa_at, expires_at, status, decided_at, status_reason, version;

-- name: GetApprovalRequest :one
SELECT id, object_type, object_key, operation, summary, change, envelope, envelope_hash, policy_version, policy_steps, requested_by,
  requested_at, requester_mfa_method, requester_mfa_at, expires_at, status, decided_at, status_reason, version
  FROM fyniq.approval_request WHERE id = sqlc.arg(id);

-- name: LockApprovalRequest :one
SELECT id, object_type, object_key, operation, summary, change, envelope, envelope_hash, policy_version, policy_steps, requested_by,
  requested_at, requester_mfa_method, requester_mfa_at, expires_at, status, decided_at, status_reason, version
  FROM fyniq.approval_request WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: ListApprovalRequests :many
SELECT id, object_type, object_key, operation, summary, change, envelope, envelope_hash, policy_version, policy_steps, requested_by,
  requested_at, requester_mfa_method, requester_mfa_at, expires_at, status, decided_at, status_reason, version
  FROM fyniq.approval_request
 WHERE sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text
 ORDER BY requested_at, id;

-- name: DecideApprovalRequest :one
UPDATE fyniq.approval_request SET status = sqlc.arg(status), decided_at = sqlc.arg(decided_at), status_reason = sqlc.narg(reason),
  version = version + 1
 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version) AND status = 'pending'
RETURNING id, object_type, object_key, operation, summary, change, envelope, envelope_hash, policy_version, policy_steps, requested_by,
  requested_at, requester_mfa_method, requester_mfa_at, expires_at, status, decided_at, status_reason, version;

-- name: VoidedApprovalRequests :many
SELECT id, version, status_reason FROM fyniq.approval_request
 WHERE voided_xact = pg_current_xact_id()::text::bigint
 ORDER BY id;

-- name: InsertApprovalSignature :one
INSERT INTO fyniq.approval_signature (request_id, step, signer, decision, envelope_hash, mfa_method, mfa_at, signed_at, reason,
  passkey_challenge_id, passkey_id, authenticator_data, client_data, assertion_signature)
VALUES (sqlc.arg(request_id), sqlc.arg(step), sqlc.arg(signer), sqlc.arg(decision), sqlc.arg(envelope_hash), sqlc.arg(mfa_method),
  sqlc.arg(mfa_at), sqlc.arg(signed_at), sqlc.narg(reason), sqlc.narg(passkey_challenge_id), sqlc.narg(passkey_id),
  sqlc.narg(authenticator_data), sqlc.narg(client_data), sqlc.narg(assertion_signature))
RETURNING id, request_id, step, signer, decision, envelope_hash, mfa_method, mfa_at, signed_at, reason, voided_at,
  passkey_challenge_id, passkey_id, authenticator_data, client_data, assertion_signature;

-- name: ApprovalSignatures :many
SELECT id, request_id, step, signer, decision, envelope_hash, mfa_method, mfa_at, signed_at, reason, voided_at,
  passkey_challenge_id, passkey_id, authenticator_data, client_data, assertion_signature
  FROM fyniq.approval_signature WHERE request_id = sqlc.arg(request_id)
 ORDER BY signed_at, id;

-- name: ListApprovalPolicies :many
SELECT id, object_type, version, steps, request_id, created_by, created_at
  FROM fyniq.approval_policy
 ORDER BY object_type, version;

-- name: InsertApprovalPolicy :one
INSERT INTO fyniq.approval_policy (object_type, version, steps, request_id, created_by, created_at)
VALUES (sqlc.arg(object_type), sqlc.arg(version), sqlc.arg(steps)::jsonb, sqlc.arg(request_id), sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING id, object_type, version, steps, request_id, created_by, created_at;
