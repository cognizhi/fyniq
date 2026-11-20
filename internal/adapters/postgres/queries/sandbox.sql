-- name: CreateSandboxOrganisation :one
-- The security-definer function that makes a sandbox organisation, its creator's membership and Owner role, and its registry row (001250).
SELECT r.membership_id::uuid AS membership_id, r.assignment_id::uuid AS assignment_id
  FROM fyniq.create_sandbox_organisation(sqlc.arg(by_user)::uuid, sqlc.narg(change_id)::uuid, sqlc.arg(organisation)::uuid, sqlc.arg(name)::text,
    'SGD'::text, sqlc.arg(at)::timestamptz) r;

-- name: OrganisationIsSandbox :one
SELECT fyniq.organisation_is_sandbox(sqlc.arg(org)::uuid)::boolean AS sandbox;
