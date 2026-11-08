-- name: InsertAuditEvent :exec
-- audit_event_append chains it: sequence, prev_hash and hash (INV-12).
INSERT INTO fyniq.audit_event (id, body) VALUES ($1, $2);

-- name: GetAuditHead :one
SELECT c.seq, c.head_hash,
       coalesce((SELECT e.action FROM fyniq.audit_event e WHERE e.seq = c.seq), '')::text AS action
FROM fyniq.audit_chain c;

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
