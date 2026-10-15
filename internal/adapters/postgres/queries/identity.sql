-- name: ClaimSetup :one
SELECT fyniq.claim_setup(sqlc.arg(code_hash)::bytea, sqlc.arg(display_name)::text, sqlc.arg(issuer)::text, sqlc.arg(subject)::text)::uuid AS user_id;

-- name: SetupDone :one
SELECT fyniq.setup_done()::boolean AS done;

-- name: UserForIdentity :one
SELECT coalesce(fyniq.user_for_identity(sqlc.arg(issuer)::text, sqlc.arg(subject)::text), '00000000-0000-0000-0000-000000000000')::uuid AS user_id;

-- name: SetSetupCode :exec
SELECT fyniq.set_setup_code(sqlc.arg(code_hash)::bytea);

-- name: InsertInstallationOrganisation :one
INSERT INTO fyniq.organisation (name, base_currency, base_minor_units, kind)
SELECT 'Installation', c.code, c.minor_units, 'installation' FROM fyniq.currencies c WHERE c.code = 'SGD'
RETURNING organisation_id;

-- name: InsertMembership :one
INSERT INTO fyniq.user_membership (user_id) VALUES (sqlc.arg(user_id))
RETURNING id, user_id, status, version;

-- name: GetMembershipByUser :one
SELECT id, user_id, status, version FROM fyniq.user_membership WHERE user_id = sqlc.arg(user_id);

-- name: InsertRoleAssignment :one
INSERT INTO fyniq.role_assignment (membership_id, role, expires_at)
VALUES (sqlc.arg(membership_id), sqlc.arg(role), sqlc.narg(expires_at))
RETURNING id, membership_id, role, expires_at, revoked_at;

-- name: RevokeRoleAssignment :one
UPDATE fyniq.role_assignment SET revoked_at = now() WHERE id = sqlc.arg(id) AND revoked_at IS NULL
RETURNING id, membership_id, role;

-- name: ListLiveRoles :many
SELECT a.id, a.membership_id, a.role, a.expires_at, a.revoked_at
  FROM fyniq.role_assignment a JOIN fyniq.user_membership m ON m.organisation_id = a.organisation_id AND m.id = a.membership_id
 WHERE m.user_id = sqlc.arg(user_id) AND m.status = 'active' AND a.revoked_at IS NULL
   AND (a.expires_at IS NULL OR a.expires_at > sqlc.arg(now))
 ORDER BY a.role;
