-- name: InsertPSPEvent :one
INSERT INTO fyniq.psp_event (id, connector_id, provider, event_id, event_type, api_version, parser_version, object_kind, object_id, payload,
  payload_sha256, received_at)
VALUES (sqlc.arg(id), sqlc.arg(connector_id), sqlc.arg(provider), sqlc.arg(event_id), sqlc.arg(event_type), sqlc.narg(api_version),
  sqlc.arg(parser_version), sqlc.narg(object_kind), sqlc.narg(object_id), sqlc.arg(payload), sqlc.arg(payload_sha256), sqlc.arg(received_at))
ON CONFLICT ON CONSTRAINT psp_event_once DO NOTHING
RETURNING id, connector_id, provider, event_id, event_type, api_version, parser_version, object_kind, object_id, payload_sha256, received_at,
  status, parked_reason, processed_at, last_error;

-- name: GetPSPEvent :one
SELECT id, connector_id, provider, event_id, event_type, api_version, parser_version, object_kind, object_id, payload_sha256, received_at,
  status, parked_reason, processed_at, last_error FROM fyniq.psp_event WHERE id = sqlc.arg(id);

-- name: GetPSPEventPayload :one
SELECT payload FROM fyniq.psp_event WHERE id = sqlc.arg(id);

-- name: SettlePSPEvent :one
UPDATE fyniq.psp_event SET status = sqlc.arg(status), parked_reason = sqlc.narg(parked_reason), processed_at = sqlc.narg(processed_at),
  last_error = sqlc.narg(last_error)
WHERE id = sqlc.arg(id)
RETURNING id, connector_id, provider, event_id, event_type, api_version, parser_version, object_kind, object_id, payload_sha256, received_at,
  status, parked_reason, processed_at, last_error;

-- name: ListPSPEvents :many
SELECT id, connector_id, provider, event_id, event_type, api_version, parser_version, object_kind, object_id, payload_sha256, received_at,
  status, parked_reason, processed_at, last_error FROM fyniq.psp_event
WHERE (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text)
  AND (sqlc.arg(connector)::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR connector_id = sqlc.arg(connector)::uuid)
  AND (sqlc.arg(before)::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR id < sqlc.arg(before)::uuid)
ORDER BY id DESC LIMIT sqlc.arg(max_rows)::int;
