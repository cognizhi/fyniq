-- name: InsertConversionStep :one
INSERT INTO fyniq.conversion_step (settlement_id, kind, on_date, reference, recorded_by, recorded_at)
VALUES (sqlc.arg(settlement_id), sqlc.arg(kind), sqlc.arg(on_date), sqlc.narg(reference), sqlc.arg(recorded_by), sqlc.arg(recorded_at))
RETURNING *;

-- name: ConversionStepsOf :many
SELECT * FROM fyniq.conversion_step WHERE settlement_id = sqlc.arg(settlement_id) ORDER BY recorded_at, id;
