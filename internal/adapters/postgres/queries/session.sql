-- name: StartSession :exec
SELECT fyniq.start_session(sqlc.arg(token_hash)::bytea, sqlc.arg(issuer)::text, sqlc.arg(subject)::text, sqlc.arg(provider)::text,
  sqlc.arg(now)::timestamptz, sqlc.arg(expires)::timestamptz, sqlc.narg(step_up_at)::timestamptz);

-- name: SessionFor :one
-- An empty issuer means no live session. Step-up is in Unix seconds, 0 when the sign-in passed none.
SELECT coalesce(s.issuer, '')::text AS issuer, coalesce(s.subject, '')::text AS subject, coalesce(s.provider, '')::text AS provider,
       coalesce(s.user_id, '00000000-0000-0000-0000-000000000000')::uuid AS user_id,
       coalesce(s.organisation_id, '00000000-0000-0000-0000-000000000000')::uuid AS organisation_id,
       coalesce(s.roles, '{}')::text[] AS roles,
       coalesce(extract(epoch FROM s.step_up_at), 0)::bigint AS step_up_unix,
       coalesce(s.expires_at, 'epoch')::timestamptz AS expires_at
  FROM fyniq.session_for(sqlc.arg(token_hash)::bytea, sqlc.arg(now)::timestamptz,
       sqlc.arg(idle_seconds)::bigint * interval '1 second') AS s;

-- name: EndSession :exec
SELECT fyniq.end_session(sqlc.arg(token_hash)::bytea, sqlc.arg(now)::timestamptz);

-- name: EndUserSessions :one
SELECT fyniq.end_user_sessions(sqlc.arg(user_id)::uuid, sqlc.arg(keep_issuer)::text, sqlc.arg(keep_subject)::text,
  sqlc.arg(removed_subjects)::text[], sqlc.arg(now)::timestamptz)::integer AS ended;
