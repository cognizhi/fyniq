-- name: SignInMethods :many
SELECT * FROM fyniq.sign_in_methods();

-- name: SignInCounts :many
SELECT provider::text AS provider, sign_ins::bigint AS sign_ins FROM fyniq.sign_in_counts(sqlc.arg(since)::timestamptz);

-- name: SetSignInMethod :one
SELECT * FROM fyniq.set_sign_in_method(sqlc.arg(by_user)::uuid, sqlc.narg(change_id)::uuid, sqlc.arg(provider)::text,
  sqlc.arg(version)::integer, sqlc.arg(enabled)::boolean, sqlc.arg(domains)::text[], sqlc.arg(join_by_domain)::boolean,
  sqlc.arg(end_sessions)::boolean, sqlc.arg(at)::timestamptz);

-- name: SignInEssential :many
SELECT p::text AS provider FROM fyniq.sign_in_essential() AS p;
