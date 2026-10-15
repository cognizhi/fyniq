-- name: InsertOutbox :one
INSERT INTO fyniq.outbox (worker_class, kind, payload, business_key, max_attempts)
VALUES (sqlc.arg(worker_class), sqlc.arg(kind), sqlc.arg(payload), sqlc.narg(business_key), sqlc.arg(max_attempts))
RETURNING id, worker_class, kind, payload, business_key, state, attempts, max_attempts, dispatches, task_name,
          lease_holder, lease_expires_at, deadline_at, last_error;

-- name: GetOutbox :one
SELECT id, worker_class, kind, payload, business_key, state, attempts, max_attempts, dispatches, task_name,
       lease_holder, lease_expires_at, deadline_at, last_error
FROM fyniq.outbox WHERE id = $1;

-- name: MarkOutboxDispatched :one
-- After the task is enqueued; a re-dispatch drops any expired lease.
UPDATE fyniq.outbox
SET state = 'dispatched', dispatches = dispatches + 1, task_name = sqlc.arg(task_name),
    deadline_at = sqlc.arg(deadline_at), lease_holder = NULL, lease_expires_at = NULL
WHERE id = sqlc.arg(id) AND state IN ('pending', 'dispatched', 'running', 'failed', 'unknown_outcome')
RETURNING id, state, dispatches;

-- name: LeaseOutbox :one
-- A handler takes the row before working; a running row is taken over only after its lease expired.
UPDATE fyniq.outbox
SET state = 'running', attempts = attempts + 1, lease_holder = sqlc.arg(holder), lease_expires_at = sqlc.arg(until)
WHERE id = sqlc.arg(id)
  AND (state IN ('dispatched', 'failed') OR (state = 'running' AND lease_expires_at < sqlc.arg(now)))
RETURNING id, worker_class, kind, payload, business_key, state, attempts, max_attempts, dispatches, task_name,
          lease_holder, lease_expires_at, deadline_at, last_error;

-- name: FinishOutbox :one
UPDATE fyniq.outbox
SET state = sqlc.arg(state), lease_holder = NULL, lease_expires_at = NULL, last_error = sqlc.narg(last_error)
WHERE id = sqlc.arg(id) AND lease_holder = sqlc.arg(holder)
RETURNING id, state, attempts;

-- name: DueOutbox :many
-- The sweep: rows never dispatched (past a grace for the after-commit dispatch), and rows dispatched,
-- running or failed past their deadline. Unknown outcomes and dead rows are never swept (§12.11).
SELECT id, worker_class, kind, payload, business_key, state, attempts, max_attempts, dispatches, task_name,
       lease_holder, lease_expires_at, deadline_at, last_error
FROM fyniq.outbox
WHERE (state = 'pending' AND created_at < sqlc.arg(pending_before))
   OR (state IN ('dispatched', 'running', 'failed') AND deadline_at < sqlc.arg(now))
ORDER BY created_at
LIMIT 500;

-- name: MarkOutboxDead :one
UPDATE fyniq.outbox SET state = 'dead', lease_holder = NULL, lease_expires_at = NULL, last_error = sqlc.narg(last_error)
WHERE id = sqlc.arg(id) AND state NOT IN ('done', 'dead')
RETURNING id, state, attempts;

-- name: OutboxAfterRestore :execrows
-- After a database restore, nothing unfinished is re-dispatched automatically: it is reconciled first.
UPDATE fyniq.outbox SET state = 'unknown_outcome', lease_holder = NULL, lease_expires_at = NULL,
       last_error = 'set by the after-restore step: reconcile before any retry'
WHERE state IN ('pending', 'dispatched', 'running', 'failed');
