-- name: InsertBillPaymentPlan :one
INSERT INTO fyniq.bill_payment_plan (bill_id, planned_on, recorded_by)
VALUES (sqlc.arg(bill_id), sqlc.narg(planned_on), sqlc.arg(recorded_by))
RETURNING id, bill_id, planned_on, recorded_by, recorded_at;

-- name: BillPaymentPlans :many
-- The latest plan of each bill named: the one in force.
SELECT DISTINCT ON (bill_id) id, bill_id, planned_on, recorded_by, recorded_at
FROM fyniq.bill_payment_plan
WHERE bill_id = ANY(sqlc.arg(bill_ids)::uuid[])
ORDER BY bill_id, recorded_at DESC, id DESC;
