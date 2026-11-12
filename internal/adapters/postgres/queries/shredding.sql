-- The shredding of person keys and the anonymisation of contacts (§11.8, §11.9; designs D9 and D10).

-- name: ListShreddingRuns :many
SELECT id, cohort, keys, listed_at, status, approved_by, approved_at, register, done_at, reapplied, version
FROM fyniq.pd_shredding_run ORDER BY cohort;

-- name: DueKeyCohorts :many
-- The financial years whose keys may be shredded: their last day before the cutoff, and no run listing them yet.
SELECT k.cohort, array_agg(k.id ORDER BY k.id)::uuid[] AS keys
FROM fyniq.person_key k
WHERE k.cohort < sqlc.arg(before)::date
  AND NOT EXISTS (SELECT 1 FROM fyniq.pd_shredding_run r WHERE r.cohort = k.cohort)
GROUP BY k.cohort ORDER BY k.cohort;

-- name: InsertShreddingRun :one
INSERT INTO fyniq.pd_shredding_run (cohort, keys, listed_at) VALUES (sqlc.arg(cohort), sqlc.arg(keys)::uuid[], sqlc.arg(listed_at))
RETURNING id, cohort, keys, listed_at, status, approved_by, approved_at, register, done_at, reapplied, version;

-- name: ApproveShreddingRun :one
UPDATE fyniq.pd_shredding_run SET status = 'approved', approved_by = sqlc.arg(approved_by), approved_at = sqlc.arg(approved_at), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version) AND status = 'listed'
RETURNING id, cohort, keys, listed_at, status, approved_by, approved_at, register, done_at, reapplied, version;

-- name: RegisterShreddingRun :exec
UPDATE fyniq.pd_shredding_run SET register = sqlc.arg(register), version = version + 1 WHERE id = sqlc.arg(id);

-- name: ShredPersonKeys :one
SELECT fyniq.shred_person_keys(sqlc.arg(id)::uuid)::integer AS shredded;

-- name: ReapplyShredding :one
SELECT fyniq.reapply_shredding(sqlc.arg(id)::uuid, sqlc.arg(cohort)::date, sqlc.arg(keys)::uuid[], sqlc.arg(listed_at)::timestamptz,
  sqlc.arg(approved_by)::text, sqlc.arg(approved_at)::timestamptz, sqlc.arg(register)::text)::integer AS shredded;

-- name: ListAnonymisations :many
SELECT id, contact_id, request_id, reason, register, anonymised_by, anonymised_at, reapplied FROM fyniq.pd_anonymisation ORDER BY anonymised_at, id;

-- name: InsertAnonymisation :one
INSERT INTO fyniq.pd_anonymisation (contact_id, request_id, reason, register, anonymised_by, anonymised_at)
VALUES (sqlc.arg(contact_id), sqlc.narg(request_id), sqlc.arg(reason), sqlc.arg(register), sqlc.arg(anonymised_by), sqlc.arg(anonymised_at))
RETURNING id;

-- name: InsertReappliedAnonymisation :one
INSERT INTO fyniq.pd_anonymisation (contact_id, request_id, reason, register, anonymised_by, anonymised_at, reapplied)
VALUES (sqlc.arg(contact_id), sqlc.narg(request_id), sqlc.arg(reason), sqlc.arg(register), sqlc.arg(anonymised_by), sqlc.arg(anonymised_at), true)
RETURNING id;

-- name: AnonymiseContact :one
SELECT fyniq.anonymise_contact(sqlc.arg(contact_id)::uuid)::integer AS changed;

-- name: ShreddedKeys :many
-- The done runs' keys among some, with each run's year and when it was done: what the explorer says of a shredded value.
SELECT k.key::uuid AS key, r.cohort, r.done_at
FROM fyniq.pd_shredding_run r, unnest(r.keys) AS k(key)
WHERE r.status = 'done' AND k.key = ANY(sqlc.arg(keys)::uuid[]);
