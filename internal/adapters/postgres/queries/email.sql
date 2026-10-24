-- name: InsertEmailMessage :one
INSERT INTO fyniq.email_message (purpose, recipient, subject) VALUES (sqlc.arg(purpose), sqlc.arg(recipient), sqlc.arg(subject))
RETURNING id, purpose, recipient, subject, status, provider, provider_id, attempts, attempt_started_at, accepted_at, last_error, created_at;

-- name: GetEmailMessage :one
SELECT id, purpose, recipient, subject, status, provider, provider_id, attempts, attempt_started_at, accepted_at, last_error, created_at FROM fyniq.email_message WHERE id = sqlc.arg(id);

-- name: ListEmailMessages :many
SELECT id, purpose, recipient, subject, status, provider, provider_id, attempts, attempt_started_at, accepted_at, last_error, created_at
FROM fyniq.email_message
WHERE (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (sqlc.narg(purpose)::text IS NULL OR purpose = sqlc.narg(purpose)::text)
  AND (sqlc.narg(before)::uuid IS NULL OR id < sqlc.narg(before)::uuid)
ORDER BY id DESC LIMIT sqlc.arg(max_rows);

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

-- name: InsertEmailEvent :one
INSERT INTO fyniq.email_event (message_id, provider, delivery_id, kind, recipient, detail, occurred_at, received_at)
VALUES (sqlc.arg(message_id), sqlc.arg(provider), sqlc.arg(delivery_id), sqlc.arg(kind), sqlc.arg(recipient), sqlc.narg(detail),
  sqlc.arg(occurred_at), sqlc.arg(received_at))
ON CONFLICT ON CONSTRAINT email_event_once DO NOTHING
RETURNING id, message_id, provider, delivery_id, kind, recipient, detail, occurred_at, received_at;

-- name: EmailEvents :many
SELECT id, message_id, provider, delivery_id, kind, recipient, detail, occurred_at, received_at FROM fyniq.email_event
WHERE message_id = sqlc.arg(message_id) ORDER BY occurred_at, id;

-- name: InsertEmailSuppression :one
INSERT INTO fyniq.email_suppression (address, reason, event_id, suppressed_at)
VALUES (sqlc.arg(address), sqlc.arg(reason), sqlc.arg(event_id), sqlc.arg(suppressed_at))
ON CONFLICT (organisation_id, address) WHERE released_at IS NULL DO NOTHING
RETURNING id, address, reason, event_id, suppressed_at, released_at, released_by;

-- name: EmailSuppressionInForce :one
SELECT id, address, reason, event_id, suppressed_at, released_at, released_by FROM fyniq.email_suppression
WHERE address = sqlc.arg(address) AND released_at IS NULL;

-- name: EmailSuppressions :many
SELECT id, address, reason, event_id, suppressed_at, released_at, released_by FROM fyniq.email_suppression
WHERE sqlc.arg(all_too)::boolean OR released_at IS NULL
ORDER BY suppressed_at DESC, id DESC
LIMIT 1000;

-- name: ReleaseEmailSuppression :one
UPDATE fyniq.email_suppression SET released_at = sqlc.arg(released_at), released_by = sqlc.arg(released_by)
WHERE id = sqlc.arg(id) AND released_at IS NULL
RETURNING id, address, reason, event_id, suppressed_at, released_at, released_by;

-- name: EmailSuppressionByID :one
SELECT id, address, reason, event_id, suppressed_at, released_at, released_by FROM fyniq.email_suppression WHERE id = sqlc.arg(id);
