-- Person keys (§11.8, design D8): each person's data key per financial year, wrapped by the deployment's key.

-- name: GetPersonKey :one
SELECT id, wrapped FROM fyniq.person_key WHERE person = sqlc.arg(person) AND cohort = sqlc.arg(cohort);

-- name: InsertPersonKey :one
-- A key made at the same time by another transaction wins: this returns no row, and the caller reads that one.
INSERT INTO fyniq.person_key (person, cohort, wrapped) VALUES (sqlc.arg(person), sqlc.arg(cohort), sqlc.arg(wrapped))
ON CONFLICT (organisation_id, person, cohort) DO NOTHING
RETURNING id;

-- name: ListPersonKeys :many
SELECT id, person, cohort, wrapped FROM fyniq.person_key WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- name: ContactIsPerson :one
-- A contact is a person when it is an individual or a director.
SELECT (individual OR is_director)::boolean AS person FROM fyniq.contact WHERE id = sqlc.arg(id);
