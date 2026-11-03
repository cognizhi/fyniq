-- name: ListAccessReviewAttestations :many
SELECT id, quarter, attested_by, attested_at, note, report, report_hash
  FROM fyniq.access_review_attestation
 ORDER BY quarter;

-- name: InsertAccessReviewAttestation :one
-- The database recomputes the report's hash (FR-CTL-06): the API cannot record one that does not match.
INSERT INTO fyniq.access_review_attestation (quarter, attested_by, attested_at, note, report, report_hash)
VALUES (sqlc.arg(quarter), sqlc.arg(attested_by), sqlc.arg(attested_at), sqlc.narg(note), sqlc.arg(report)::text,
        sha256(convert_to(sqlc.arg(report)::text, 'UTF8')))
RETURNING id, quarter, attested_by, attested_at, note, report, report_hash;
