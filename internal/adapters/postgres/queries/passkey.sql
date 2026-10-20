-- name: IssuePasskeyChallenge :one
SELECT fyniq.webauthn_challenge_issue(sqlc.arg(session_hash)::bytea, sqlc.arg(purpose)::text, sqlc.arg(challenge)::bytea,
  sqlc.arg(now)::timestamptz, sqlc.arg(expires_at)::timestamptz)::uuid AS id;

-- name: TakePasskeyChallenge :one
-- No row taken comes back as an empty challenge.
SELECT coalesce(t.challenge, '\x'::bytea)::bytea AS challenge,
       coalesce(t.user_id, '00000000-0000-0000-0000-000000000000')::uuid AS user_id
  FROM fyniq.webauthn_challenge_take(sqlc.arg(id)::uuid, sqlc.arg(session_hash)::bytea, sqlc.arg(purpose)::text,
       sqlc.arg(now)::timestamptz) AS t;

-- name: ListPasskeys :many
SELECT c.id, c.user_id, c.credential_id, c.public_key, c.sign_count, c.aaguid, c.transports, c.backup_eligible, c.backup_state,
       c.name, c.created_at, c.last_used_at, c.revoked_at
  FROM fyniq.webauthn_credentials(sqlc.arg(user_id)::uuid, sqlc.arg(include_revoked)::boolean) AS c;

-- name: RegisterPasskey :one
SELECT fyniq.webauthn_credential_register(sqlc.arg(challenge_id)::uuid, sqlc.arg(session_hash)::bytea, sqlc.arg(credential_id)::bytea,
  sqlc.arg(public_key)::bytea, sqlc.arg(sign_count)::bigint, sqlc.arg(aaguid)::bytea, sqlc.arg(transports)::text[],
  sqlc.arg(backup_eligible)::boolean, sqlc.arg(backup_state)::boolean, sqlc.arg(name)::text, sqlc.arg(now)::timestamptz)::uuid AS id;

-- name: StepUpSessionWithPasskey :exec
SELECT fyniq.session_step_up(sqlc.arg(challenge_id)::uuid, sqlc.arg(session_hash)::bytea, sqlc.arg(passkey_id)::uuid,
  sqlc.arg(sign_count)::bigint, sqlc.arg(backup_state)::boolean, sqlc.arg(now)::timestamptz);

-- name: RevokePasskey :one
SELECT fyniq.webauthn_credential_revoke(sqlc.arg(user_id)::uuid, sqlc.arg(id)::uuid, sqlc.arg(now)::timestamptz)::boolean AS revoked;

-- name: IssueApprovalChallenge :one
SELECT fyniq.webauthn_approval_challenge_issue(sqlc.arg(session_hash)::bytea, sqlc.arg(request_id)::uuid, sqlc.arg(envelope_hash)::bytea,
  sqlc.arg(nonce)::bytea, sqlc.arg(now)::timestamptz, sqlc.arg(expires_at)::timestamptz)::uuid AS id;

-- name: AnswerApprovalChallenge :exec
SELECT fyniq.webauthn_approval_answer(sqlc.arg(challenge_id)::uuid, sqlc.arg(session_hash)::bytea, sqlc.arg(passkey_id)::uuid,
  sqlc.arg(sign_count)::bigint, sqlc.arg(backup_state)::boolean, sqlc.arg(now)::timestamptz);
