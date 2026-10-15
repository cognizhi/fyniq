-- name: ClaimJobSlot :one
SELECT fyniq.claim_job_slot(sqlc.arg(job)::text, sqlc.arg(slot)::timestamptz, sqlc.arg(holder)::text,
                            sqlc.arg(until)::timestamptz, sqlc.arg(now)::timestamptz)::boolean AS claimed;

-- name: SkipJobSlots :one
SELECT fyniq.skip_job_slots(sqlc.arg(job)::text, sqlc.arg(slots)::timestamptz[], sqlc.arg(now)::timestamptz)::int AS skipped;

-- name: StartedJobSlot :exec
SELECT fyniq.started_job_slot(sqlc.arg(job)::text, sqlc.arg(slot)::timestamptz, sqlc.arg(execution)::text);

-- name: FinishJobSlot :one
SELECT fyniq.finish_job_slot(sqlc.arg(job)::text, sqlc.arg(slot)::timestamptz, sqlc.arg(ok)::boolean,
                             sqlc.arg(detail)::text, sqlc.arg(now)::timestamptz)::boolean AS finished;

-- name: LatestJobSlot :one
-- The epoch when the job never ran.
SELECT coalesce(fyniq.latest_job_slot(sqlc.arg(job)::text), 'epoch'::timestamptz)::timestamptz AS slot;

-- name: Beat :exec
SELECT fyniq.beat(sqlc.arg(name)::text, sqlc.arg(at)::timestamptz, sqlc.arg(detail)::text);

-- name: ListHeartbeats :many
SELECT name, last_beat FROM fyniq.heartbeats ORDER BY name;

-- name: ListJobSlots :many
SELECT job, slot, state, coalesce(execution, '')::text AS execution, coalesce(detail, '')::text AS detail
FROM fyniq.job_slots WHERE job = $1 ORDER BY slot;
