-- name: InsertEmailMessage :one
INSERT INTO fyniq.email_message (purpose, recipient, subject) VALUES (sqlc.arg(purpose), sqlc.arg(recipient), sqlc.arg(subject))
RETURNING id, purpose, recipient, subject, status, provider, provider_id, attempts, attempt_started_at, accepted_at, last_error, created_at;

-- name: GetEmailMessage :one
SELECT id, purpose, recipient, subject, status, provider, provider_id, attempts, attempt_started_at, accepted_at, last_error, created_at FROM fyniq.email_message WHERE id = sqlc.arg(id);

-- name: LockEmailMessage :one
SELECT id, purpose, recipient, subject, status, provider, provider_id, attempts, attempt_started_at, accepted_at, last_error, created_at FROM fyniq.email_message WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: StartEmailAttempt :one
UPDATE fyniq.email_message SET attempts = attempts + 1, attempt_started_at = sqlc.arg(now) WHERE id = sqlc.arg(id) AND status = 'queued'
RETURNING id, purpose, recipient, subject, status, provider, provider_id, attempts, attempt_started_at, accepted_at, last_error, created_at;

-- name: FinishEmailMessage :one
UPDATE fyniq.email_message SET status = sqlc.arg(status), provider = sqlc.narg(provider), provider_id = sqlc.narg(provider_id),
  accepted_at = sqlc.narg(accepted_at), last_error = sqlc.narg(last_error)
WHERE id = sqlc.arg(id) AND status = 'queued'
RETURNING id, purpose, recipient, subject, status, provider, provider_id, attempts, attempt_started_at, accepted_at, last_error, created_at;

-- name: ReleaseEmailAttempt :one
UPDATE fyniq.email_message SET attempt_started_at = NULL, last_error = sqlc.narg(last_error) WHERE id = sqlc.arg(id) AND status = 'queued'
RETURNING id, purpose, recipient, subject, status, provider, provider_id, attempts, attempt_started_at, accepted_at, last_error, created_at;
