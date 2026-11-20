-- name: InsertRetentionHold :one
-- A hold, placed in force (AUD-11, 001300); the unique indexes refuse a second on one subject.
INSERT INTO fyniq.retention_hold (kind, document_id, contact_id, year_number, year_starts_on, year_ends_on, reason, placed_by, placed_at)
VALUES (sqlc.arg(kind), sqlc.narg(document_id), sqlc.narg(contact_id), sqlc.narg(year_number), sqlc.narg(year_starts_on), sqlc.narg(year_ends_on),
        sqlc.arg(reason), sqlc.arg(placed_by), sqlc.arg(placed_at))
RETURNING id, kind, document_id, contact_id, year_number, year_starts_on, year_ends_on, reason, placed_by, placed_at, lifted_by, lifted_at, lift_reason;

-- name: LiftRetentionHold :one
-- A hold lifted once, with its reason; no row when it does not exist or was lifted already.
UPDATE fyniq.retention_hold SET lifted_by = sqlc.arg(lifted_by), lifted_at = sqlc.arg(lifted_at), lift_reason = sqlc.arg(lift_reason)
WHERE id = sqlc.arg(id) AND lifted_at IS NULL
RETURNING id, kind, document_id, contact_id, year_number, year_starts_on, year_ends_on, reason, placed_by, placed_at, lifted_by, lifted_at, lift_reason;

-- name: GetRetentionHold :one
SELECT id, kind, document_id, contact_id, year_number, year_starts_on, year_ends_on, reason, placed_by, placed_at, lifted_by, lifted_at, lift_reason
FROM fyniq.retention_hold WHERE id = sqlc.arg(id);

-- name: ListRetentionHolds :many
-- The holds, the newest placed first; the ones in force only when asked.
SELECT id, kind, document_id, contact_id, year_number, year_starts_on, year_ends_on, reason, placed_by, placed_at, lifted_by, lifted_at, lift_reason
FROM fyniq.retention_hold
WHERE NOT sqlc.arg(in_force_only)::boolean OR lifted_at IS NULL
ORDER BY placed_at DESC, id DESC;

-- name: HeldLedgerDates :many
-- The posting dates the holds in force cover (fyniq.hold_ledger_dates, 001300).
SELECT d::date AS day FROM fyniq.hold_ledger_dates(fyniq.current_org()) AS d ORDER BY 1;

-- name: HeldAuditEvents :many
-- The sequence numbers from first to last the holds in force cover (fyniq.hold_covers_event, 001300).
SELECT e.seq FROM fyniq.audit_event e
WHERE e.seq BETWEEN sqlc.arg(first)::bigint AND sqlc.arg(last)::bigint
  AND fyniq.hold_covers_event(e.organisation_id, e.object_id, (e.body::jsonb ->> 'occurred_at')::timestamptz)
ORDER BY e.seq;

-- name: HeldEvidenceFiles :many
-- The final evidence files the holds in force cover (fyniq.hold_evidence, 001300).
SELECT ev.id AS evidence_id, ev.locked_key::text AS locked_key FROM fyniq.evidence ev
WHERE ev.id IN (SELECT h FROM fyniq.hold_evidence(fyniq.current_org()) AS h)
ORDER BY ev.id;

-- name: FreeEvidenceFiles :many
-- The final evidence files no hold in force covers.
SELECT ev.id AS evidence_id, ev.locked_key::text AS locked_key FROM fyniq.evidence ev
WHERE ev.state = 'final' AND ev.locked_key IS NOT NULL
  AND ev.id NOT IN (SELECT h FROM fyniq.hold_evidence(fyniq.current_org()) AS h)
ORDER BY ev.id;
