-- name: PeopleLinkAccess :one
SELECT id, issuer, subject, people_access, payroll_approver, version, received_at FROM fyniq.people_link_access
WHERE issuer = sqlc.arg(issuer) AND subject = sqlc.arg(subject);

-- name: PeopleLinkAccessList :many
SELECT id, issuer, subject, people_access, payroll_approver, version, received_at FROM fyniq.people_link_access ORDER BY received_at, id;

-- name: SavePeopleLinkAccess :one
INSERT INTO fyniq.people_link_access (issuer, subject, people_access, payroll_approver, version, received_at)
VALUES (sqlc.arg(issuer), sqlc.arg(subject), sqlc.arg(people_access), sqlc.arg(payroll_approver), sqlc.arg(version), sqlc.arg(received_at))
ON CONFLICT (organisation_id, issuer, subject) DO UPDATE
SET people_access = EXCLUDED.people_access, payroll_approver = EXCLUDED.payroll_approver, version = EXCLUDED.version, received_at = EXCLUDED.received_at
WHERE fyniq.people_link_access.version < EXCLUDED.version
RETURNING id;
