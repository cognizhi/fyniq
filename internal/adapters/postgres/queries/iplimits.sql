-- name: CountIPRequest :one
SELECT fyniq.count_ip_request(sqlc.arg(client)::text, sqlc.arg(kind)::text, sqlc.arg(window_start)::timestamptz)::integer AS used;

-- name: PurgeIPRateWindows :one
SELECT fyniq.purge_ip_rate_windows(sqlc.arg(before)::timestamptz)::bigint AS purged;
