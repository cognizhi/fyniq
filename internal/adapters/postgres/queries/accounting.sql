-- name: InsertAccountingFramework :one
-- The next framework version; the unique key refuses a second one with the same number.
INSERT INTO fyniq.accounting_framework (version, from_on, framework, edition, written_by)
SELECT coalesce(max(f.version), 0) + 1, sqlc.arg(from_on)::date, sqlc.arg(framework)::text, sqlc.arg(edition)::text,
       sqlc.arg(written_by)::text
FROM fyniq.accounting_framework f
RETURNING id, version, from_on, framework, edition, written_by, written_at, confirmed_by, confirmed_at;

-- name: ListAccountingFrameworks :many
SELECT id, version, from_on, framework, edition, written_by, written_at, confirmed_by, confirmed_at
FROM fyniq.accounting_framework
ORDER BY version;

-- name: ConfirmAccountingFramework :one
UPDATE fyniq.accounting_framework SET confirmed_by = $2, confirmed_at = now()
WHERE version = $1
RETURNING id, version, from_on, framework, edition, written_by, written_at, confirmed_by, confirmed_at;

-- name: InsertAccountingPolicy :one
-- The area's next policy version; the unique key refuses a second one with the same number.
INSERT INTO fyniq.accounting_policy (area, version, from_on, wording, written_by)
SELECT sqlc.arg(area)::text, coalesce(max(p.version), 0) + 1, sqlc.arg(from_on)::date, sqlc.arg(wording)::text,
       sqlc.arg(written_by)::text
FROM fyniq.accounting_policy p
WHERE p.area = sqlc.arg(area)::text
RETURNING id, area, version, from_on, wording, written_by, written_at, approved_by, approved_at;

-- name: ListAccountingPolicies :many
SELECT id, area, version, from_on, wording, written_by, written_at, approved_by, approved_at
FROM fyniq.accounting_policy
ORDER BY area, version;

-- name: ApproveAccountingPolicy :one
UPDATE fyniq.accounting_policy SET approved_by = $3, approved_at = now()
WHERE area = $1 AND version = $2
RETURNING id, area, version, from_on, wording, written_by, written_at, approved_by, approved_at;
