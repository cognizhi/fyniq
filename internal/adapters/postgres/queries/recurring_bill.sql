-- name: InsertRecurringBill :one
INSERT INTO fyniq.recurring_bill (name, status, every_months, day_of_month, starts_on, ends_on, next_on)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, version, content_version;

-- name: InsertRecurringBillVersion :exec
INSERT INTO fyniq.recurring_bill_version (recurring_bill_id, version, content, edited_by) VALUES ($1, $2, $3, $4);

-- name: GetRecurringBill :one
SELECT r.id, r.name, r.status, r.every_months, r.day_of_month, r.starts_on, r.ends_on, r.next_on, r.version,
       r.content_version, r.last_on, r.last_bill_id, v.content, v.edited_by, v.edited_at
FROM fyniq.recurring_bill r
JOIN fyniq.recurring_bill_version v
  ON v.organisation_id = r.organisation_id AND v.recurring_bill_id = r.id AND v.version = r.content_version
WHERE r.id = $1;

-- name: LockRecurringBill :one
-- The recurring bill as it stands, locked for its change.
SELECT r.id, r.name, r.status, r.every_months, r.day_of_month, r.starts_on, r.ends_on, r.next_on, r.version,
       r.content_version, r.last_on, r.last_bill_id, v.content, v.edited_by, v.edited_at
FROM fyniq.recurring_bill r
JOIN fyniq.recurring_bill_version v
  ON v.organisation_id = r.organisation_id AND v.recurring_bill_id = r.id AND v.version = r.content_version
WHERE r.id = $1
FOR UPDATE OF r;

-- name: UpdateRecurringBill :exec
UPDATE fyniq.recurring_bill
SET name = $2, status = $3, every_months = $4, day_of_month = $5, starts_on = $6, ends_on = $7, next_on = $8,
    version = $9, content_version = $10
WHERE id = $1;

-- name: PageRecurringBills :many
-- By name, then ID, after the cursor's (name, id).
SELECT r.id, r.name, r.status, r.every_months, r.day_of_month, r.starts_on, r.ends_on, r.next_on, r.version,
       r.content_version, r.last_on, r.last_bill_id, v.content, v.edited_by, v.edited_at
FROM fyniq.recurring_bill r
JOIN fyniq.recurring_bill_version v
  ON v.organisation_id = r.organisation_id AND v.recurring_bill_id = r.id AND v.version = r.content_version
WHERE sqlc.narg(after_id)::uuid IS NULL OR (r.name, r.id) > (sqlc.arg(after_name)::text, sqlc.narg(after_id)::uuid)
ORDER BY r.name, r.id
LIMIT sqlc.arg(page_size);

-- name: GetRecurringBillVersion :one
SELECT version, content, edited_by, edited_at FROM fyniq.recurring_bill_version
WHERE recurring_bill_id = $1 AND version = $2;

-- name: DueRecurringBills :many
-- Active recurring bills whose next occurrence is on or before a day, earliest first (FR-AP-09, FR-AUT-01).
SELECT id FROM fyniq.recurring_bill
WHERE status = 'active' AND next_on IS NOT NULL AND next_on <= sqlc.arg(on_day)::date
ORDER BY next_on, id
LIMIT sqlc.arg(max_rows);

-- name: CreateScheduledBillDraft :one
-- The scheduler's one way to make a bill: the draft of a recurring bill's next occurrence (FY057).
SELECT fyniq.create_scheduled_bill_draft(sqlc.arg(recurring_bill_id)::uuid, sqlc.arg(on_day)::date)::uuid AS id;

-- name: SetRecurringBillNext :exec
-- The worker moves the next occurrence once its draft is made, and changes nothing else.
UPDATE fyniq.recurring_bill SET next_on = sqlc.narg(next_on)::date WHERE id = sqlc.arg(id);
