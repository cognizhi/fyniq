-- name: InsertEmailMessage :one
INSERT INTO fyniq.email_message (purpose, recipient, subject) VALUES (sqlc.arg(purpose), sqlc.arg(recipient), sqlc.arg(subject))
RETURNING id, purpose, recipient, subject, status, provider, provider_id, attempts, attempt_started_at, accepted_at, last_error, created_at;

-- name: GetEmailMessage :one
SELECT id, purpose, recipient, subject, status, provider, provider_id, attempts, attempt_started_at, accepted_at, last_error, created_at FROM fyniq.email_message WHERE id = sqlc.arg(id);

-- name: LockEmailMessage :one
SELECT id, purpose, recipient, subject, status, provider, provider_id, attempts, attempt_started_at, accepted_at, last_error, created_at FROM fyniq.email_message WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: StartEmailAttempt :one
UPDATE fyniq.email_message SET attempts = attempts + 1, attempt_started_at = sqlc.arg(now), provider = sqlc.arg(provider)
WHERE id = sqlc.arg(id) AND status = 'queued'
RETURNING id, purpose, recipient, subject, status, provider, provider_id, attempts, attempt_started_at, accepted_at, last_error, created_at;

-- name: FinishEmailMessage :one
UPDATE fyniq.email_message SET status = sqlc.arg(status), provider = sqlc.narg(provider), provider_id = sqlc.narg(provider_id),
  accepted_at = sqlc.narg(accepted_at), last_error = sqlc.narg(last_error)
WHERE id = sqlc.arg(id) AND status = 'queued'
RETURNING id, purpose, recipient, subject, status, provider, provider_id, attempts, attempt_started_at, accepted_at, last_error, created_at;

-- name: ReleaseEmailAttempt :one
UPDATE fyniq.email_message SET attempt_started_at = NULL, last_error = sqlc.narg(last_error) WHERE id = sqlc.arg(id) AND status = 'queued'
RETURNING id, purpose, recipient, subject, status, provider, provider_id, attempts, attempt_started_at, accepted_at, last_error, created_at;

-- name: InsertEmailSender :one
INSERT INTO fyniq.email_sender (purpose, address, display_name, set_by, set_at)
VALUES (sqlc.arg(purpose), sqlc.narg(address), sqlc.narg(display_name), sqlc.arg(set_by), sqlc.arg(set_at))
RETURNING id, purpose, address, display_name, set_by, set_at;

-- name: EmailSendersInForce :many
-- The latest choice of each purpose: the one in force.
SELECT DISTINCT ON (purpose) id, purpose, address, display_name, set_by, set_at
FROM fyniq.email_sender
ORDER BY purpose, set_at DESC, id DESC;
