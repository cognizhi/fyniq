-- name: InsertWebhookEndpoint :one
INSERT INTO fyniq.webhook_endpoint (id, url, label, event_types, secret_sealed, secret_hint, created_by, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(url), sqlc.arg(label), sqlc.arg(event_types)::text[], sqlc.arg(secret_sealed)::bytea, sqlc.arg(secret_hint), sqlc.arg(created_by), sqlc.arg(at), sqlc.arg(at))
RETURNING id, url, label, event_types, state, secret_sealed, secret_hint, previous_secret_sealed, previous_secret_until, challenge_hash, verified_at, failure_run, paused_reason, version, created_by, created_at, updated_at;

-- name: GetWebhookEndpoint :one
SELECT id, url, label, event_types, state, secret_sealed, secret_hint, previous_secret_sealed, previous_secret_until, challenge_hash, verified_at, failure_run, paused_reason, version, created_by, created_at, updated_at
FROM fyniq.webhook_endpoint WHERE id = sqlc.arg(id);

-- name: LockWebhookEndpoint :one
SELECT id, url, label, event_types, state, secret_sealed, secret_hint, previous_secret_sealed, previous_secret_until, challenge_hash, verified_at, failure_run, paused_reason, version, created_by, created_at, updated_at
FROM fyniq.webhook_endpoint WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: ListWebhookEndpoints :many
SELECT id, url, label, event_types, state, secret_sealed, secret_hint, previous_secret_sealed, previous_secret_until, challenge_hash, verified_at, failure_run, paused_reason, version, created_by, created_at, updated_at
FROM fyniq.webhook_endpoint ORDER BY created_at, id;

-- name: SubscribedWebhookEndpoints :many
SELECT id, url, label, event_types, state, secret_sealed, secret_hint, previous_secret_sealed, previous_secret_until, challenge_hash, verified_at, failure_run, paused_reason, version, created_by, created_at, updated_at
FROM fyniq.webhook_endpoint WHERE state = 'active' AND sqlc.arg(event_type)::text = ANY (event_types) ORDER BY created_at, id;

-- name: ChangeWebhookEndpoint :one
UPDATE fyniq.webhook_endpoint SET
  url = coalesce(sqlc.narg(url)::text, url),
  label = coalesce(sqlc.narg(label)::text, label),
  event_types = coalesce(sqlc.narg(event_types)::text[], event_types),
  state = coalesce(sqlc.narg(state)::text, state),
  secret_sealed = coalesce(sqlc.narg(secret_sealed)::bytea, secret_sealed),
  secret_hint = coalesce(sqlc.narg(secret_hint)::text, secret_hint),
  previous_secret_sealed = CASE WHEN sqlc.arg(clear_previous)::bool THEN NULL ELSE coalesce(sqlc.narg(previous_sealed)::bytea, previous_secret_sealed) END,
  previous_secret_until = CASE WHEN sqlc.arg(clear_previous)::bool THEN NULL ELSE coalesce(sqlc.narg(previous_until)::timestamptz, previous_secret_until) END,
  challenge_hash = CASE WHEN sqlc.arg(clear_challenge)::bool THEN NULL ELSE coalesce(sqlc.narg(challenge_hash)::text, challenge_hash) END,
  verified_at = CASE WHEN sqlc.arg(clear_verified)::bool THEN NULL ELSE coalesce(sqlc.narg(verified_at)::timestamptz, verified_at) END,
  paused_reason = CASE WHEN sqlc.arg(clear_paused)::bool THEN NULL ELSE coalesce(sqlc.narg(paused_reason)::text, paused_reason) END,
  failure_run = CASE WHEN sqlc.arg(reset_failures)::bool THEN 0 ELSE failure_run END,
  version = version + 1, updated_at = sqlc.arg(at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING id, url, label, event_types, state, secret_sealed, secret_hint, previous_secret_sealed, previous_secret_until, challenge_hash, verified_at, failure_run, paused_reason, version, created_by, created_at, updated_at;

-- name: CountWebhookFailure :one
-- A failed delivery counts against its endpoint; this touches no audited column.
UPDATE fyniq.webhook_endpoint SET failure_run = failure_run + 1, updated_at = sqlc.arg(at) WHERE id = sqlc.arg(id) RETURNING failure_run;

-- name: PauseWebhookEndpoint :exec
-- Ten failed deliveries in a row pause an active endpoint (design D3).
UPDATE fyniq.webhook_endpoint SET state = 'paused', paused_reason = 'Ten deliveries failed in a row', version = version + 1, updated_at = sqlc.arg(at) WHERE id = sqlc.arg(id) AND state = 'active';

-- name: ClearWebhookFailures :exec
UPDATE fyniq.webhook_endpoint SET failure_run = 0, updated_at = sqlc.arg(at) WHERE id = sqlc.arg(id) AND failure_run > 0;

-- name: DisableWebhookEndpoint :exec
-- The endpoint answered 410 Gone: an active endpoint is disabled.
UPDATE fyniq.webhook_endpoint SET state = 'disabled', paused_reason = 'The endpoint answered 410 Gone', version = version + 1, updated_at = sqlc.arg(at) WHERE id = sqlc.arg(id) AND state = 'active';

-- name: InsertWebhookDelivery :one
INSERT INTO fyniq.webhook_delivery (endpoint_id, event_id, event_type, message_id, payload, next_attempt_at, created_at)
VALUES (sqlc.arg(endpoint_id), sqlc.arg(event_id), sqlc.arg(event_type), sqlc.arg(message_id), sqlc.arg(payload)::jsonb, sqlc.arg(at), sqlc.arg(at))
ON CONFLICT (organisation_id, endpoint_id, event_id) DO NOTHING
RETURNING id, endpoint_id, event_id, event_type, message_id, payload, state, attempts, replays, next_attempt_at, first_attempt_at, last_attempt_at, last_status, last_error, delivered_at, created_at;

-- name: GetWebhookDelivery :one
SELECT id, endpoint_id, event_id, event_type, message_id, payload, state, attempts, replays, next_attempt_at, first_attempt_at, last_attempt_at, last_status, last_error, delivered_at, created_at
FROM fyniq.webhook_delivery WHERE id = sqlc.arg(id);

-- name: DueWebhookDeliveries :many
SELECT d.id, d.endpoint_id, d.event_id, d.event_type, d.message_id, d.payload, d.state, d.attempts, d.replays, d.next_attempt_at, d.first_attempt_at, d.last_attempt_at, d.last_status, d.last_error, d.delivered_at, d.created_at
FROM fyniq.webhook_delivery d JOIN fyniq.webhook_endpoint e ON e.organisation_id = d.organisation_id AND e.id = d.endpoint_id
WHERE d.state = 'pending' AND d.next_attempt_at <= sqlc.arg(now) AND e.state = 'active' ORDER BY d.next_attempt_at, d.id LIMIT sqlc.arg(max_rows);

-- name: ListWebhookDeliveries :many
SELECT id, endpoint_id, event_id, event_type, message_id, payload, state, attempts, replays, next_attempt_at, first_attempt_at, last_attempt_at, last_status, last_error, delivered_at, created_at
FROM fyniq.webhook_delivery
WHERE endpoint_id = sqlc.arg(endpoint_id) AND (sqlc.arg(state)::text = '' OR state = sqlc.arg(state)::text) AND (sqlc.narg(after)::uuid IS NULL OR id < sqlc.narg(after)::uuid)
ORDER BY id DESC LIMIT sqlc.arg(max_rows);

-- name: ListWebhookAttempts :many
SELECT id, delivery_id, number, at, status, error, duration_ms FROM fyniq.webhook_attempt WHERE delivery_id = sqlc.arg(delivery_id) ORDER BY number;

-- name: InsertWebhookAttempt :exec
INSERT INTO fyniq.webhook_attempt (delivery_id, number, at, status, error, duration_ms) VALUES (sqlc.arg(delivery_id), sqlc.arg(number), sqlc.arg(at), sqlc.narg(status), sqlc.narg(error), sqlc.arg(duration_ms));

-- name: RecordWebhookResult :one
UPDATE fyniq.webhook_delivery SET
  state = CASE WHEN sqlc.arg(delivered)::bool THEN 'delivered' WHEN sqlc.arg(dead)::bool THEN 'dead' ELSE 'pending' END,
  attempts = attempts + 1,
  next_attempt_at = coalesce(sqlc.narg(next_at)::timestamptz, next_attempt_at),
  first_attempt_at = coalesce(first_attempt_at, sqlc.arg(at)::timestamptz),
  last_attempt_at = sqlc.arg(at)::timestamptz,
  last_status = sqlc.narg(status)::integer,
  last_error = sqlc.narg(error)::text,
  delivered_at = CASE WHEN sqlc.arg(delivered)::bool THEN sqlc.arg(at)::timestamptz ELSE NULL END
WHERE id = sqlc.arg(id) AND state = 'pending'
RETURNING id, endpoint_id, event_id, event_type, message_id, payload, state, attempts, replays, next_attempt_at, first_attempt_at, last_attempt_at, last_status, last_error, delivered_at, created_at;

-- name: ReplayWebhookDelivery :one
UPDATE fyniq.webhook_delivery SET state = 'pending', replays = replays + 1, next_attempt_at = sqlc.arg(at) WHERE id = sqlc.arg(id) AND state = 'dead'
RETURNING id, endpoint_id, event_id, event_type, message_id, payload, state, attempts, replays, next_attempt_at, first_attempt_at, last_attempt_at, last_status, last_error, delivered_at, created_at;
