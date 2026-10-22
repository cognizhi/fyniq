-- name: ListMembers :many
SELECT m.id, m.user_id, m.status, m.version
  FROM fyniq.user_membership m
 WHERE m.status = 'active'
 ORDER BY m.id;

-- name: MemberNames :many
SELECT n.user_id::uuid AS user_id, n.display_name::text AS display_name FROM fyniq.member_names() AS n;

-- name: LiveRoleAssignments :many
SELECT a.id, a.membership_id, a.role, a.expires_at
  FROM fyniq.role_assignment a
 WHERE a.revoked_at IS NULL AND (a.expires_at IS NULL OR a.expires_at > sqlc.arg(now))
 ORDER BY a.membership_id, a.role;

-- name: LockMembers :exec
SELECT pg_advisory_xact_lock(hashtextextended('fyniq.members:' || fyniq.current_org()::text, 0));

-- name: CountActiveMembers :one
SELECT count(*) FROM fyniq.user_membership WHERE status = 'active';

-- name: InsertMemberInvitation :one
-- Both times come from the API's clock (000073), as an idempotency key's do.
INSERT INTO fyniq.member_invitation (code_hash, display_name, roles, roles_expire_at, expires_at, created_at)
VALUES (sqlc.arg(code_hash), sqlc.arg(display_name), sqlc.arg(roles)::text[], sqlc.narg(roles_expire_at), sqlc.arg(expires_at), sqlc.arg(created_at))
RETURNING id, display_name, roles, roles_expire_at, created_at, expires_at, accepted_at, withdrawn_at;

-- name: PendingMemberInvitations :many
SELECT id, display_name, roles, roles_expire_at, created_at, expires_at, accepted_at, withdrawn_at
  FROM fyniq.member_invitation
 WHERE accepted_at IS NULL AND withdrawn_at IS NULL AND expires_at > sqlc.arg(now)
 ORDER BY created_at, id;

-- name: WithdrawMemberInvitation :one
UPDATE fyniq.member_invitation SET withdrawn_at = sqlc.arg(now)
 WHERE id = sqlc.arg(id) AND accepted_at IS NULL AND withdrawn_at IS NULL
RETURNING id, display_name, roles, roles_expire_at, created_at, expires_at, accepted_at, withdrawn_at;

-- name: AcceptMemberInvitation :one
SELECT r.organisation_id::uuid AS organisation_id, r.invitation_id::uuid AS invitation_id, r.user_id::uuid AS user_id,
       r.new_user::boolean AS new_user, r.membership_id::uuid AS membership_id
  FROM fyniq.accept_member_invitation(sqlc.arg(code_hash)::bytea, sqlc.arg(display_name)::text, sqlc.arg(issuer)::text,
       sqlc.arg(subject)::text, sqlc.arg(now)::timestamptz) AS r;

-- name: GetMemberInvitation :one
SELECT id, display_name, roles, roles_expire_at, created_at, expires_at, accepted_at, withdrawn_at
  FROM fyniq.member_invitation WHERE id = sqlc.arg(id);
