-- name: UserRecoveryCodes :one
SELECT unused::integer AS unused, coalesce(extract(epoch FROM created_at), 0)::bigint AS created_unix
FROM fyniq.user_recovery_codes(sqlc.arg(user_id)::uuid);

-- name: SetUserRecoveryCodes :one
SELECT fyniq.set_user_recovery_codes(sqlc.arg(session_hash)::bytea, sqlc.arg(hashes)::bytea[], sqlc.arg(now)::timestamptz)::uuid AS user_id;

-- name: ConfirmAccountRecovery :one
SELECT fyniq.confirm_account_recovery(sqlc.arg(by_user)::uuid, sqlc.arg(for_user)::uuid, sqlc.arg(org)::uuid, sqlc.arg(code_hash)::bytea,
  sqlc.arg(now)::timestamptz, sqlc.arg(expires)::timestamptz)::uuid AS id;

-- name: RecoverAccount :one
SELECT recovery_id::uuid AS recovery_id, user_id::uuid AS user_id, confirmed_by::uuid AS confirmed_by, confirming_organisation::uuid AS confirming_organisation,
  cooling_off_until::timestamptz AS cooling_off_until
FROM fyniq.recover_account(sqlc.arg(session_hash)::bytea, sqlc.arg(code_hash)::bytea, sqlc.arg(confirmation_hash)::bytea,
  sqlc.arg(new_hashes)::bytea[], sqlc.arg(now)::timestamptz, sqlc.arg(cooling_seconds)::bigint * interval '1 second');
