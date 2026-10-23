-- name: InsertWithholdingDecision :one
INSERT INTO fyniq.withholding_decision (supplier_id, nature, outcome, rate_percent, tax_agent, note, bill_id, recorded_by)
VALUES (sqlc.arg(supplier_id), sqlc.arg(nature), sqlc.arg(outcome), sqlc.narg(rate_percent)::text::numeric, sqlc.arg(tax_agent), sqlc.narg(note),
  sqlc.narg(bill_id), sqlc.arg(recorded_by))
RETURNING id, supplier_id, nature, outcome, coalesce(trim_scale(rate_percent)::text, '')::text AS rate_percent, tax_agent, note, bill_id, recorded_by, recorded_at;

-- name: WithholdingDecisionInForce :one
-- The latest decision for the supplier and nature recorded at or before the time given (any, when it is null).
SELECT id, supplier_id, nature, outcome, coalesce(trim_scale(rate_percent)::text, '')::text AS rate_percent, tax_agent, note, bill_id, recorded_by, recorded_at
FROM fyniq.withholding_decision
WHERE supplier_id = sqlc.arg(supplier_id) AND nature = sqlc.arg(nature)
  AND (sqlc.narg(at)::timestamptz IS NULL OR recorded_at <= sqlc.narg(at)::timestamptz)
ORDER BY recorded_at DESC, id DESC
LIMIT 1;

-- name: WithholdingDecisionsOf :many
SELECT id, supplier_id, nature, outcome, coalesce(trim_scale(rate_percent)::text, '')::text AS rate_percent, tax_agent, note, bill_id, recorded_by, recorded_at
FROM fyniq.withholding_decision
WHERE supplier_id = sqlc.arg(supplier_id)
ORDER BY recorded_at DESC, id DESC
LIMIT 500;
