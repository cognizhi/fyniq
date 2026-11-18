-- name: AIBudgetsUsed :many
SELECT b.*, (CASE WHEN b.scope IN ('tokens_organisation', 'tokens_person') THEN
               (SELECT coalesce(sum(CASE WHEN r.state = 'settled' THEN coalesce(r.actual_tokens, 0) ELSE coalesce(r.tokens, 0) END), 0)::text
                  FROM fyniq.ai_reservation r
                 WHERE r.month = sqlc.arg(month) AND (b.scope = 'tokens_organisation' OR r.person = b.subject))
             ELSE
               (SELECT coalesce(sum(CASE WHEN r.state = 'settled' THEN r.actual ELSE coalesce(r.amount, 0) END), 0)::text
                  FROM fyniq.ai_reservation r
                 WHERE r.month = sqlc.arg(month)
                   AND (b.scope = 'organisation' OR (b.scope = 'agent' AND r.agent = b.subject)
                        OR (b.scope = 'connection' AND r.connection_id::text = b.subject) OR (b.scope = 'person' AND r.person = b.subject)))
             END)::text AS used
FROM fyniq.ai_budget b ORDER BY b.scope, b.subject;

-- name: GetAIBudget :one
SELECT * FROM fyniq.ai_budget WHERE scope = sqlc.arg(scope) AND subject = sqlc.arg(subject);

-- name: InsertAIBudget :one
INSERT INTO fyniq.ai_budget (scope, subject, amount, set_by, set_at)
VALUES (sqlc.arg(scope), sqlc.arg(subject), sqlc.narg(amount)::numeric, sqlc.arg(set_by), sqlc.arg(set_at))
RETURNING *;

-- name: UpdateAIBudget :one
UPDATE fyniq.ai_budget SET amount = sqlc.narg(amount)::numeric, version = version + 1, set_by = sqlc.arg(set_by), set_at = sqlc.arg(set_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: ReserveAI :one
SELECT fyniq.ai_reserve(sqlc.arg(run), sqlc.arg(step)::integer, sqlc.arg(agent), sqlc.arg(connection), sqlc.arg(person), sqlc.arg(month),
  sqlc.narg(amount)::numeric, sqlc.arg(tokens)::bigint)::text AS reservation;

-- name: SettleAI :exec
SELECT fyniq.ai_settle(sqlc.arg(reservation), sqlc.narg(actual)::numeric, sqlc.arg(actual_tokens)::bigint, sqlc.arg(known)::boolean);
