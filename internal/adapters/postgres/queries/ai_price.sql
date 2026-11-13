-- name: RecordAIPriceCatalogue :one
SELECT fyniq.record_ai_price_catalogue(sqlc.arg(source), sqlc.arg(url), sqlc.arg(fetched_at), sqlc.arg(sha256), sqlc.arg(size)::bigint,
  sqlc.narg(licence), sqlc.arg(licence_url), sqlc.arg(licence_sha256), sqlc.arg(outcome), sqlc.narg(failure), sqlc.arg(entries)::integer,
  sqlc.arg(document))::uuid AS id;

-- name: GetAIPriceCatalogue :one
SELECT * FROM fyniq.ai_price_catalogues WHERE id = sqlc.arg(id);

-- name: LatestAIPriceCatalogue :one
SELECT * FROM fyniq.ai_price_catalogues WHERE source = sqlc.arg(source) AND outcome = 'valid' ORDER BY fetched_at DESC, id DESC LIMIT 1;

-- name: AIPriceCatalogues :many
SELECT * FROM fyniq.ai_price_catalogues ORDER BY fetched_at DESC, id DESC LIMIT sqlc.arg(page_size);

-- name: InsertAIPrice :one
INSERT INTO fyniq.ai_price (connection_id, model, number, source, catalogue_id, catalogue_key, match, currency, ignored, status, review,
  review_detail, recorded_by, recorded_at)
VALUES (sqlc.arg(connection_id), sqlc.arg(model),
  (SELECT coalesce(max(p.number), 0) + 1 FROM fyniq.ai_price p WHERE p.connection_id = sqlc.arg(connection_id) AND p.model = sqlc.arg(model)),
  sqlc.arg(source), sqlc.narg(catalogue_id), sqlc.narg(catalogue_key), sqlc.arg(match), sqlc.arg(currency), sqlc.arg(ignored)::text[],
  sqlc.arg(status), sqlc.narg(review), sqlc.narg(review_detail), sqlc.arg(recorded_by), sqlc.arg(recorded_at))
RETURNING *;

-- name: InsertAIPriceRate :exec
INSERT INTO fyniq.ai_price_rate (price_id, kind, above, unit, amount)
VALUES (sqlc.arg(price_id), sqlc.arg(kind), sqlc.arg(above), sqlc.arg(unit), sqlc.arg(amount)::numeric);

-- name: CurrentAIPrices :many
SELECT * FROM fyniq.ai_price p WHERE p.connection_id = sqlc.arg(connection_id)
  AND (p.status IN ('active', 'pending') OR (p.status = 'declined' AND p.number = (SELECT max(q.number) FROM fyniq.ai_price q
    WHERE q.connection_id = p.connection_id AND q.model = p.model AND q.status = 'declined')))
ORDER BY p.model, p.number;

-- name: GetAIPrice :one
SELECT * FROM fyniq.ai_price WHERE id = sqlc.arg(id);

-- name: AIPriceRates :many
SELECT price_id, kind, above, unit, amount::text AS amount FROM fyniq.ai_price_rate
WHERE price_id = ANY(sqlc.arg(price_ids)::uuid[]) ORDER BY price_id, kind, above;

-- name: DecideAIPrice :one
UPDATE fyniq.ai_price SET status = sqlc.arg(status), decided_by = sqlc.arg(decided_by), decided_at = sqlc.arg(decided_at)
WHERE id = sqlc.arg(id) RETURNING *;

-- name: CheckAIModelPrice :execrows
UPDATE fyniq.ai_model SET price_checked_at = sqlc.arg(price_checked_at), unpriced = sqlc.narg(unpriced)
WHERE connection_id = sqlc.arg(connection_id) AND model = sqlc.arg(model);
