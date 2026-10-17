-- name: ClaimIdempotencyKey :one
INSERT INTO fyniq.idempotency_key (client_id, key, request_hash, created_at, expires_at)
VALUES (sqlc.arg(client_id), sqlc.arg(key), sqlc.arg(request_hash), sqlc.arg(created_at), sqlc.arg(expires_at))
ON CONFLICT (organisation_id, client_id, key) DO NOTHING
RETURNING id;

-- name: GetIdempotencyKey :one
SELECT request_hash, response_status, response_body, expires_at FROM fyniq.idempotency_key
WHERE client_id = sqlc.arg(client_id) AND key = sqlc.arg(key);

-- name: CompleteIdempotencyKey :execrows
UPDATE fyniq.idempotency_key SET response_status = sqlc.arg(response_status), response_body = sqlc.arg(response_body)
WHERE client_id = sqlc.arg(client_id) AND key = sqlc.arg(key);

-- name: PurgeIdempotencyKeys :execrows
DELETE FROM fyniq.idempotency_key WHERE expires_at <= sqlc.arg(now);
