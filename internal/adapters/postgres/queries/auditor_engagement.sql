-- name: ListAuditEngagements :many
SELECT * FROM fyniq.auditor_engagement ORDER BY opened_at DESC, id DESC;

-- name: GetAuditEngagement :one
SELECT * FROM fyniq.auditor_engagement WHERE id = sqlc.arg(id);

-- name: InsertAuditEngagement :one
INSERT INTO fyniq.auditor_engagement (name, firm, auditor_user_id, period_from, period_to, opens_on, closes_on, opened_by, opened_at)
VALUES (sqlc.arg(name), sqlc.arg(firm), sqlc.arg(auditor_user_id), sqlc.arg(period_from), sqlc.arg(period_to), sqlc.arg(opens_on), sqlc.arg(closes_on), sqlc.arg(opened_by), sqlc.arg(opened_at))
RETURNING *;

-- name: ConfirmAuditEngagement :one
UPDATE fyniq.auditor_engagement SET status = 'active', confirmed_by = sqlc.arg(confirmed_by), confirmed_at = sqlc.arg(confirmed_at)
WHERE id = sqlc.arg(id) AND status = 'pending'
RETURNING *;

-- name: CloseAuditEngagement :one
UPDATE fyniq.auditor_engagement SET status = 'closed', closed_by = sqlc.arg(closed_by), closed_at = sqlc.arg(closed_at)
WHERE id = sqlc.arg(id) AND status IN ('pending', 'active')
RETURNING *;

-- name: ListAuditRequests :many
SELECT * FROM fyniq.auditor_request WHERE engagement_id = sqlc.arg(engagement_id) ORDER BY number;

-- name: GetAuditRequest :one
SELECT * FROM fyniq.auditor_request WHERE id = sqlc.arg(id);

-- name: NextAuditRequestNumber :one
SELECT (COALESCE(max(number), 0) + 1)::int4 FROM fyniq.auditor_request WHERE engagement_id = sqlc.arg(engagement_id);

-- name: InsertAuditRequest :one
INSERT INTO fyniq.auditor_request (engagement_id, number, title, detail, due_on, assignee_user_id, requested_by, requested_at)
VALUES (sqlc.arg(engagement_id), sqlc.arg(number), sqlc.arg(title), sqlc.arg(detail), sqlc.arg(due_on), sqlc.arg(assignee_user_id), sqlc.arg(requested_by), sqlc.arg(requested_at))
RETURNING *;

-- name: AnswerAuditRequest :one
UPDATE fyniq.auditor_request SET status = 'answered', answer = sqlc.arg(answer), evidence_ids = sqlc.arg(evidence_ids), answered_by = sqlc.arg(answered_by), answered_at = sqlc.arg(answered_at)
WHERE id = sqlc.arg(id) AND status IN ('open', 'answered')
RETURNING *;

-- name: CloseAuditRequest :one
UPDATE fyniq.auditor_request SET status = 'closed', closed_by = sqlc.arg(closed_by), closed_at = sqlc.arg(closed_at)
WHERE id = sqlc.arg(id) AND status = 'answered'
RETURNING *;

-- name: ListAuditSamples :many
SELECT * FROM fyniq.auditor_sample WHERE engagement_id = sqlc.arg(engagement_id) ORDER BY drawn_at DESC, id DESC;

-- name: InsertAuditSample :one
INSERT INTO fyniq.auditor_sample (engagement_id, rule, seed, population, items, drawn_by, drawn_at)
VALUES (sqlc.arg(engagement_id), sqlc.arg(rule), sqlc.arg(seed), sqlc.arg(population), sqlc.arg(items), sqlc.arg(drawn_by), sqlc.arg(drawn_at))
RETURNING *;
