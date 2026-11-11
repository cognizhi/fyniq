-- name: InsertAuditEvent :exec
-- audit_event_append chains it: sequence, prev_hash and hash (INV-12).
INSERT INTO fyniq.audit_event (id, body) VALUES ($1, $2);

-- name: GetAuditHead :one
-- Through audit_chain_head (000286), which every service may call: the API reads the head for a close's snapshot.
SELECT h.seq, h.head_hash, h.action FROM fyniq.audit_chain_head() h;

-- name: ListAuditEvents :many
SELECT id, seq, body, prev_hash, hash FROM fyniq.audit_event
WHERE seq > sqlc.arg(after) ORDER BY seq LIMIT sqlc.arg(max);

-- name: ListTenants :many
SELECT organisation_id FROM fyniq.tenants ORDER BY created_at, organisation_id;

-- name: InsertAuditRequestDetail :exec
-- A security event's request details, beside the chain (AUD-06, 000285).
INSERT INTO fyniq.audit_request_detail (id, occurred_at, ip, user_agent) VALUES ($1, $2, $3, $4);

-- name: ListAuditRequestDetails :many
SELECT id, occurred_at, ip, user_agent FROM fyniq.audit_request_detail WHERE id = ANY(sqlc.arg(event_ids)::uuid[]);

-- name: CountAuditRequestDetailsBefore :one
SELECT count(*) FROM fyniq.audit_request_detail WHERE occurred_at < $1;

-- name: EraseAuditRequestDetails :one
SELECT erased, first_seq, last_seq FROM fyniq.audit_erase_details($1);

-- name: FindAuditEvents :many
-- The explorer's events (FR-CTL-03), newest first, each filter optional (000288 indexes the actor, the record and the
-- action). An action ending in "." matches every action it starts.
SELECT e.id, e.seq, e.body, e.prev_hash, e.hash FROM fyniq.audit_event e
WHERE (sqlc.narg(actor)::text IS NULL OR e.body::jsonb -> 'actor' ->> 'id' = sqlc.narg(actor)::text)
  AND (sqlc.narg(originator)::text IS NULL
       OR (sqlc.narg(originator)::text = 'any' AND e.body::jsonb ->> 'originator' IS NOT NULL)
       OR e.body::jsonb ->> 'originator' = sqlc.narg(originator)::text)
  AND (sqlc.narg(object_type)::text IS NULL OR e.object_type = sqlc.narg(object_type)::text)
  AND (sqlc.narg(object_id)::uuid IS NULL OR e.object_id = sqlc.narg(object_id)::uuid)
  AND (sqlc.narg(field)::text IS NULL OR jsonb_exists(e.body::jsonb -> 'changes', sqlc.narg(field)::text))
  AND (sqlc.narg(action)::text IS NULL OR e.action = sqlc.narg(action)::text
       OR (right(sqlc.narg(action)::text, 1) = '.' AND starts_with(e.action, sqlc.narg(action)::text)))
  AND (sqlc.narg(approval)::text IS NULL OR jsonb_exists(e.body::jsonb -> 'approval_ids', sqlc.narg(approval)::text))
  AND (sqlc.narg(from_at)::timestamptz IS NULL OR (e.body::jsonb ->> 'occurred_at')::timestamptz >= sqlc.narg(from_at)::timestamptz)
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR (e.body::jsonb ->> 'occurred_at')::timestamptz < sqlc.narg(to_at)::timestamptz)
  AND (sqlc.arg(before)::bigint = 0 OR e.seq < sqlc.arg(before)::bigint)
ORDER BY e.seq DESC
LIMIT sqlc.arg(max);

-- name: GetAuditEventByID :one
SELECT id, seq, body, prev_hash, hash FROM fyniq.audit_event WHERE id = $1;

-- name: InsertAuditBoundary :one
-- A purge boundary, written before the purge it bounds (AUD-09, 000291).
INSERT INTO fyniq.audit_boundary (last_seq, last_hash, last_occurred_at, approval, counts, anchor_key, written_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, last_seq, last_hash, last_occurred_at, approval, counts, anchor_key, written_at;

-- name: ListAuditBoundaries :many
SELECT id, last_seq, last_hash, last_occurred_at, approval, counts, anchor_key, written_at FROM fyniq.audit_boundary ORDER BY last_seq;

-- name: PurgeAudit :one
SELECT fyniq.audit_purge($1)::bigint AS purged;
