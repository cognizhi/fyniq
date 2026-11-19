-- What projects have billed (p1-projects task 5.2).

-- name: BilledItems :many
SELECT kind, ref_id, line_no, invoice_id FROM fyniq.project_billed_item WHERE project_id = sqlc.arg(project_id) ORDER BY created_at, id;

-- name: BillableDocuments :many
-- Posted bills and claims with a billable line that carries the project as a dimension value (FR-PRJ-08), by ID after a cursor.
SELECT d.id
FROM fyniq.document d
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
WHERE d.type IN ('bill', 'expense_claim') AND d.status = 'posted'
  AND EXISTS (
    SELECT 1 FROM jsonb_array_elements(v.payload->'lines') l
    WHERE l->>'billable' = 'true' AND (l->'dimension_value_ids') ? sqlc.arg(project_id)::text)
  AND (sqlc.narg(after_id)::uuid IS NULL OR d.id > sqlc.narg(after_id)::uuid)
ORDER BY d.id
LIMIT sqlc.arg(max_rows);

-- name: ProjectMilestones :many
SELECT m.id, m.project_id, m.kind, m.name, m.amount, m.due_on, m.accepted_on, m.version, m.created_at, b.invoice_id AS billed_on
FROM fyniq.project_milestone m
LEFT JOIN fyniq.project_billed_item b ON b.organisation_id = m.organisation_id AND b.kind = 'milestone' AND b.ref_id = m.id
WHERE m.project_id = sqlc.arg(project_id)
ORDER BY m.created_at, m.id;

-- name: ProjectMilestone :one
SELECT m.id, m.project_id, m.kind, m.name, m.amount, m.due_on, m.accepted_on, m.version, m.created_at, b.invoice_id AS billed_on
FROM fyniq.project_milestone m
LEFT JOIN fyniq.project_billed_item b ON b.organisation_id = m.organisation_id AND b.kind = 'milestone' AND b.ref_id = m.id
WHERE m.id = sqlc.arg(id);

-- name: InsertProjectMilestone :one
INSERT INTO fyniq.project_milestone (project_id, kind, name, amount, due_on)
VALUES (sqlc.arg(project_id), sqlc.arg(kind), sqlc.arg(name), sqlc.arg(amount)::numeric, sqlc.narg(due_on))
RETURNING id;

-- name: AcceptProjectMilestone :one
UPDATE fyniq.project_milestone SET accepted_on = sqlc.arg(accepted_on), version = version + 1, changed_at = now()
 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING id;
