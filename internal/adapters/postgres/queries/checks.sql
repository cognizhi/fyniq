-- name: ListCheckStates :many
SELECT * FROM fyniq.check_state ORDER BY key;

-- name: GetCheckState :one
SELECT * FROM fyniq.check_state WHERE key = sqlc.arg(key);

-- name: InsertCheckState :one
INSERT INTO fyniq.check_state (key, status, item_count, items, detail, fix, needs, setting, run_at, run_trigger, ledger_revision,
  changed_at)
VALUES (sqlc.arg(key), sqlc.arg(status), sqlc.arg(item_count), sqlc.arg(items), sqlc.narg(detail), sqlc.narg(fix), sqlc.narg(needs),
        sqlc.narg(setting), sqlc.arg(run_at), sqlc.arg(run_trigger), sqlc.arg(ledger_revision), sqlc.arg(changed_at))
RETURNING *;

-- name: UpdateCheckState :one
UPDATE fyniq.check_state SET status = sqlc.arg(status), item_count = sqlc.arg(item_count), items = sqlc.arg(items),
  detail = sqlc.narg(detail), fix = sqlc.narg(fix), needs = sqlc.narg(needs), setting = sqlc.narg(setting), run_at = sqlc.arg(run_at),
  run_trigger = sqlc.arg(run_trigger), ledger_revision = sqlc.arg(ledger_revision), changed_at = sqlc.arg(changed_at),
  version = version + 1
WHERE key = sqlc.arg(key) AND version = sqlc.arg(version)
RETURNING *;

-- name: InsertCheckRun :one
INSERT INTO fyniq.check_run (run_trigger, requested_by, started_at, finished_at, ledger_revision, audit_seq, keys, results)
VALUES (sqlc.arg(run_trigger), sqlc.narg(requested_by), sqlc.arg(started_at), sqlc.arg(finished_at), sqlc.arg(ledger_revision),
        sqlc.arg(audit_seq), sqlc.arg(keys), sqlc.narg(results))
RETURNING *;

-- name: LatestCheckRun :one
SELECT * FROM fyniq.check_run
WHERE (sqlc.arg(run_trigger)::text = '' OR run_trigger = sqlc.arg(run_trigger)::text)
ORDER BY finished_at DESC, id DESC LIMIT 1;

-- name: CheckWatermark :one
SELECT coalesce(max(audit_seq), 0)::bigint AS audit_seq FROM fyniq.check_run;

-- name: PurgeCheckRuns :execrows
DELETE FROM fyniq.check_run
WHERE (run_trigger = 'nightly' AND finished_at < sqlc.arg(nightly_before)) OR (run_trigger <> 'nightly' AND finished_at < sqlc.arg(before));

-- name: ListCheckOwners :many
SELECT * FROM fyniq.check_owner ORDER BY key;

-- name: GetCheckOwner :one
SELECT * FROM fyniq.check_owner WHERE key = sqlc.arg(key);

-- name: InsertCheckOwner :one
INSERT INTO fyniq.check_owner (key, person_user_id, set_by, set_at)
VALUES (sqlc.arg(key), sqlc.narg(person_user_id), sqlc.arg(set_by), sqlc.arg(set_at))
RETURNING *;

-- name: UpdateCheckOwner :one
UPDATE fyniq.check_owner SET person_user_id = sqlc.narg(person_user_id), set_by = sqlc.arg(set_by), set_at = sqlc.arg(set_at),
  version = version + 1
WHERE key = sqlc.arg(key) AND version = sqlc.arg(version)
RETURNING *;

-- name: OutboxWaiting :many
SELECT id, worker_class, kind, payload, business_key, state, attempts, max_attempts, dispatches, task_name,
       lease_holder, lease_expires_at, deadline_at, last_error
FROM fyniq.outbox WHERE kind = sqlc.arg(kind) AND state IN ('pending', 'dispatched')
ORDER BY created_at, id LIMIT 1;

-- name: ListCheckReviews :many
SELECT * FROM fyniq.check_review ORDER BY key, reviewed_at;

-- name: InsertCheckReview :one
INSERT INTO fyniq.check_review (key, item_kind, item_id, item_version, note, reviewed_by, reviewed_at)
VALUES (sqlc.arg(key), sqlc.arg(item_kind), sqlc.arg(item_id), sqlc.arg(item_version), sqlc.narg(note), sqlc.arg(reviewed_by),
        sqlc.arg(reviewed_at))
ON CONFLICT (organisation_id, key, item_kind, item_id, item_version) DO NOTHING
RETURNING *;
