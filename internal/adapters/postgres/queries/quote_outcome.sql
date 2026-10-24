-- name: InsertQuoteOutcome :one
INSERT INTO fyniq.quote_outcome (document_id, outcome, on_date, name, note, recorded_by)
VALUES (sqlc.arg(document_id), sqlc.arg(outcome), sqlc.arg(on_date)::date, sqlc.narg(name), sqlc.narg(note), sqlc.arg(recorded_by))
RETURNING id, document_id, outcome, on_date, name, note, recorded_by, recorded_at;

-- name: QuoteOutcomesOf :many
-- The outcomes of the quotes and orders named, if any.
SELECT id, document_id, outcome, on_date, name, note, recorded_by, recorded_at
FROM fyniq.quote_outcome
WHERE document_id = ANY (sqlc.arg(document_ids)::uuid[]);
