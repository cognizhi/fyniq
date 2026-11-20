-- name: PeriodOfDate :one
SELECT id, starts_on, ends_on, status, closed_revision, version FROM fyniq.period WHERE daterange(starts_on, ends_on, '[]') @> sqlc.arg(day)::date;

-- name: ListModuleLocks :many
SELECT id, period_id, module, locked, version, changed_by, changed_at FROM fyniq.period_module_lock ORDER BY period_id, module;

-- name: LockedModulesOfPeriod :many
SELECT module FROM fyniq.period_module_lock WHERE period_id = sqlc.arg(period_id) AND locked ORDER BY module;

-- name: SetModuleLock :one
INSERT INTO fyniq.period_module_lock (period_id, module, locked, changed_by, changed_at)
VALUES (sqlc.arg(period_id), sqlc.arg(module), sqlc.arg(locked), sqlc.arg(by), sqlc.arg(at))
ON CONFLICT (organisation_id, period_id, module) DO UPDATE
  SET locked = EXCLUDED.locked, version = fyniq.period_module_lock.version + 1, changed_by = EXCLUDED.changed_by, changed_at = EXCLUDED.changed_at
RETURNING id, period_id, module, locked, version, changed_by, changed_at;

-- name: InsertSoftLockPass :one
INSERT INTO fyniq.soft_lock_pass (period_id, user_id, reason, created_at, expires_at)
VALUES (sqlc.arg(period_id), sqlc.arg(user_id), sqlc.arg(reason), sqlc.arg(created_at), sqlc.arg(expires_at))
RETURNING id, period_id, user_id, reason, created_at, expires_at;

-- name: LiveSoftLockPass :one
SELECT id, period_id, user_id, reason, created_at, expires_at FROM fyniq.soft_lock_pass
WHERE period_id = sqlc.arg(period_id) AND user_id = sqlc.arg(user_id) AND created_at <= sqlc.arg(at) AND expires_at > sqlc.arg(at)
ORDER BY expires_at DESC, id LIMIT 1;

-- name: InsertSoftLockException :one
INSERT INTO fyniq.soft_lock_exception (pass_id, period_id, entry_id, user_id, scope, source_type, posting_date, reason, created_at)
VALUES (sqlc.arg(pass_id), sqlc.arg(period_id), sqlc.arg(entry_id), sqlc.arg(user_id), sqlc.arg(scope), sqlc.arg(source_type), sqlc.arg(posting_date), sqlc.arg(reason), sqlc.arg(created_at))
RETURNING id, pass_id, period_id, entry_id, user_id, scope, source_type, posting_date, reason, created_at;

-- name: ListSoftLockExceptions :many
SELECT id, pass_id, period_id, entry_id, user_id, scope, source_type, posting_date, reason, created_at FROM fyniq.soft_lock_exception
ORDER BY created_at DESC, id LIMIT sqlc.arg(max_rows);
