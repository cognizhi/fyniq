-- name: CountRequest :one
-- Adds one request to the client's window and returns how many the window now holds (FR-INT-16).
INSERT INTO fyniq.rate_limit_window (client_id, kind, window_start)
VALUES (sqlc.arg(client_id), sqlc.arg(kind), sqlc.arg(window_start))
ON CONFLICT (organisation_id, client_id, kind, window_start) DO UPDATE SET used = fyniq.rate_limit_window.used + 1
RETURNING used;

-- name: PurgeRateLimitWindows :execrows
DELETE FROM fyniq.rate_limit_window WHERE window_start < sqlc.arg(before);
