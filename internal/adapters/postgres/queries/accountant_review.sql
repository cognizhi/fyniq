-- name: InsertAccountantReview :one
INSERT INTO fyniq.accountant_review (period_id, close_id, opened_by, opened_at)
VALUES (sqlc.arg(period_id)::uuid, sqlc.arg(close_id)::uuid, sqlc.arg(opened_by)::uuid, sqlc.arg(opened_at)::timestamptz)
RETURNING *;

-- name: GetAccountantReview :one
-- With the close it covers: when it was attested, and its snapshot's hash (AUD-04).
SELECT sqlc.embed(r), c.attested_at AS closed_at, c.snapshot_hash
FROM fyniq.accountant_review r
JOIN fyniq.period_close c ON c.organisation_id = r.organisation_id AND c.id = r.close_id
WHERE r.id = sqlc.arg(id);

-- name: ListAccountantReviews :many
SELECT sqlc.embed(r), c.attested_at AS closed_at, c.snapshot_hash
FROM fyniq.accountant_review r
JOIN fyniq.period_close c ON c.organisation_id = r.organisation_id AND c.id = r.close_id
ORDER BY r.opened_at, r.id;

-- name: ListAccountantReviewQueries :many
SELECT * FROM fyniq.accountant_review_query ORDER BY asked_at, id;

-- name: ListAccountantReviewJournals :many
SELECT * FROM fyniq.accountant_review_journal ORDER BY linked_at, id;

-- name: BumpAccountantReview :one
-- A query, an answer or a journal moves the review's version while it is in review.
UPDATE fyniq.accountant_review SET version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version) AND status = 'in_review'
RETURNING *;

-- name: InsertAccountantReviewQuery :one
INSERT INTO fyniq.accountant_review_query (review_id, asked_by, asked_at, question)
VALUES (sqlc.arg(review_id)::uuid, sqlc.arg(asked_by)::uuid, sqlc.arg(asked_at)::timestamptz, sqlc.arg(question)::text)
RETURNING id;

-- name: AnswerAccountantReviewQuery :one
UPDATE fyniq.accountant_review_query SET answered_by = sqlc.arg(answered_by)::uuid, answered_at = sqlc.arg(answered_at)::timestamptz,
  answer = sqlc.arg(answer)::text
WHERE id = sqlc.arg(id) AND review_id = sqlc.arg(review_id) AND answer IS NULL
RETURNING id;

-- name: InsertAccountantReviewJournal :exec
INSERT INTO fyniq.accountant_review_journal (review_id, journal_id, linked_by, linked_at)
VALUES (sqlc.arg(review_id)::uuid, sqlc.arg(journal_id)::uuid, sqlc.arg(linked_by)::uuid, sqlc.arg(linked_at)::timestamptz);

-- name: SignOffAccountantReview :one
UPDATE fyniq.accountant_review SET status = 'signed_off', signed_by = sqlc.arg(signed_by)::uuid, signed_at = sqlc.arg(signed_at)::timestamptz,
  statement = sqlc.arg(statement)::text, version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version) AND status = 'in_review'
RETURNING *;

-- name: SupersedeAccountantReviews :many
UPDATE fyniq.accountant_review SET status = 'superseded', superseded_at = sqlc.arg(superseded_at)::timestamptz,
  reopen_id = sqlc.arg(reopen_id)::uuid, version = version + 1
WHERE close_id = sqlc.arg(close_id) AND status <> 'superseded'
RETURNING id, version;
