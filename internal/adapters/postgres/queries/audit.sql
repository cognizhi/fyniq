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
