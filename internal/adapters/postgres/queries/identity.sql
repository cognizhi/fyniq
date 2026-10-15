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

-- name: InstallationOrg :one
SELECT coalesce(fyniq.installation_org(), '00000000-0000-0000-0000-000000000000')::uuid AS organisation_id;

-- name: SuperAdminForIdentity :one
SELECT coalesce(fyniq.super_admin_for_identity(sqlc.arg(issuer)::text, sqlc.arg(subject)::text), '00000000-0000-0000-0000-000000000000')::uuid AS user_id;

-- name: CountSuperAdmins :one
SELECT fyniq.count_super_admins()::integer AS active;

-- name: InviteSuperAdmin :exec
SELECT fyniq.invite_super_admin(sqlc.arg(by_user)::uuid, sqlc.arg(code_hash)::bytea, sqlc.arg(now)::timestamptz, sqlc.arg(expires_at)::timestamptz);

-- name: SetRecoveryCodes :exec
SELECT fyniq.set_recovery_codes(sqlc.arg(user_id)::uuid, sqlc.arg(code_hashes)::bytea[], sqlc.arg(now)::timestamptz);

-- name: ConfirmSuperAdminRecovery :exec
SELECT fyniq.confirm_super_admin_recovery(sqlc.arg(by_user)::uuid, sqlc.arg(for_user)::uuid, sqlc.arg(code_hash)::bytea,
  sqlc.arg(now)::timestamptz, sqlc.arg(expires_at)::timestamptz);

-- name: IssueDeploymentRecovery :exec
SELECT fyniq.issue_deployment_recovery(sqlc.arg(user_id)::uuid, sqlc.arg(code_hash)::bytea, sqlc.arg(operators)::text,
  sqlc.arg(now)::timestamptz, sqlc.arg(expires_at)::timestamptz);

-- name: AcceptSuperAdminInvitation :one
SELECT fyniq.accept_super_admin_invitation(sqlc.arg(code_hash)::bytea, sqlc.arg(display_name)::text, sqlc.arg(issuer)::text,
  sqlc.arg(subject)::text, sqlc.arg(now)::timestamptz)::uuid AS user_id;

-- name: RecoverSuperAdmin :one
SELECT r.recovered::uuid AS user_id, r.via::text AS via, r.removed_subjects::text[] AS removed_subjects
  FROM fyniq.recover_super_admin(sqlc.narg(recovery_hash)::bytea, sqlc.arg(confirmation_hash)::bytea, sqlc.arg(issuer)::text,
       sqlc.arg(subject)::text, sqlc.arg(now)::timestamptz) AS r;
