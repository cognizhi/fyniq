-- name: UsageAIByMonth :many
-- AI calls by the month they started in, with their tokens and, in each currency a price used, their cost (FR-ADM-05).
SELECT date_trunc('month', started_at AT TIME ZONE 'UTC')::date AS month, count(*)::bigint AS calls, coalesce(sum(input_tokens), 0)::bigint AS input_tokens,
       coalesce(sum(output_tokens), 0)::bigint AS output_tokens, coalesce(currency, '') AS currency, coalesce(sum(cost), 0)::text AS cost
FROM fyniq.ai_call WHERE started_at >= sqlc.arg(since)
GROUP BY 1, coalesce(currency, '') ORDER BY 1, 5;

-- name: UsageExportsByMonth :many
SELECT date_trunc('month', requested_at AT TIME ZONE 'UTC')::date AS month, count(*)::bigint AS exports, coalesce(sum(size_bytes), 0)::bigint AS bytes
FROM fyniq.report_export WHERE requested_at >= sqlc.arg(since) AND status = 'ready'
GROUP BY 1 ORDER BY 1;

-- name: UsageStoredEvidence :one
SELECT count(*)::bigint AS files, coalesce(sum(size_bytes), 0)::bigint AS bytes FROM fyniq.evidence;

-- name: UsageStoredCaptures :one
SELECT count(*)::bigint AS files, coalesce(sum(size_bytes), 0)::bigint AS bytes FROM fyniq.document_capture;

-- name: UsageStoredExports :one
SELECT count(*)::bigint AS files, coalesce(sum(size_bytes), 0)::bigint AS bytes FROM fyniq.report_export WHERE status = 'ready';

-- name: UsageEvidenceByMonth :many
SELECT date_trunc('month', uploaded_at AT TIME ZONE 'UTC')::date AS month, count(*)::bigint AS files, coalesce(sum(size_bytes), 0)::bigint AS bytes
FROM fyniq.evidence WHERE uploaded_at >= sqlc.arg(since)
GROUP BY 1 ORDER BY 1;
