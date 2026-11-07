-- name: SetSessionEmail :exec
SELECT fyniq.set_session_email(sqlc.arg(token_hash)::bytea, sqlc.arg(email)::text);

-- name: JoinOffer :many
SELECT organisation_id::uuid AS organisation_id, domain::text AS domain, email::text AS email, provider::text AS provider
FROM fyniq.join_offer(sqlc.arg(session_hash)::bytea, sqlc.arg(now)::timestamptz);

-- name: JoinOfferName :one
SELECT coalesce(fyniq.join_offer_name(sqlc.arg(org)::uuid), '')::text AS name;

-- name: RequestToJoin :one
SELECT organisation_id::uuid AS organisation_id, request_id::uuid AS request_id
FROM fyniq.request_to_join(sqlc.arg(session_hash)::bytea, sqlc.arg(name)::text, sqlc.arg(now)::timestamptz);

-- name: DecideJoinRequest :one
SELECT user_id::uuid AS user_id, membership_id::uuid AS membership_id, role_assignment_id::uuid AS role_assignment_id
FROM fyniq.decide_join_request(sqlc.arg(by_user)::uuid, sqlc.arg(id)::uuid, sqlc.arg(approve)::boolean, sqlc.arg(now)::timestamptz);

-- name: JoinRequests :many
SELECT * FROM fyniq.join_request ORDER BY requested_at DESC, id;

-- name: ClaimJoinDomain :exec
SELECT fyniq.claim_join_domain(sqlc.arg(by_user)::uuid, sqlc.arg(org)::uuid, sqlc.arg(domain)::text, sqlc.arg(now)::timestamptz);

-- name: ReleaseJoinDomain :one
SELECT fyniq.release_join_domain(sqlc.arg(org)::uuid, sqlc.arg(domain)::text)::boolean AS released;

-- name: JoinDomains :many
SELECT * FROM fyniq.join_domains(sqlc.arg(org)::uuid);
