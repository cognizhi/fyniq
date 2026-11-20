-- name: CreatePracticeOrganisation :one
-- The security-definer function that opens a practice organisation: it checks that the person is an Owner of the source and holds
-- no practice organisation, and makes the organisation, their membership and Owner role (000711).
SELECT r.organisation_id::uuid AS organisation_id, r.membership_id::uuid AS membership_id, r.assignment_id::uuid AS assignment_id
  FROM fyniq.create_practice_organisation(sqlc.arg(user_id)::uuid, sqlc.arg(source)::uuid, sqlc.arg(name)::text, sqlc.arg(currency)::text, sqlc.arg(at)::timestamptz) r;

-- name: OrganisationIsPractice :one
SELECT fyniq.organisation_is_practice(sqlc.arg(org)::uuid)::boolean AS practice;
