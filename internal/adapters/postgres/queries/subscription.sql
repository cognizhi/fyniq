-- name: InsertSubscriptionContract :one
INSERT INTO fyniq.subscription_contract (contact_id, name, currency, starts_on, ends_on, mrr, created_by, created_at)
VALUES (sqlc.arg(contact_id), sqlc.arg(name), sqlc.arg(currency), sqlc.arg(starts_on), sqlc.narg(ends_on), sqlc.arg(mrr)::numeric, sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING id, contact_id, name, currency, starts_on, ends_on, mrr::text, version, created_by, created_at;

-- name: GetSubscriptionContract :one
SELECT id, contact_id, name, currency, starts_on, ends_on, mrr::text, version, created_by, created_at
FROM fyniq.subscription_contract WHERE id = sqlc.arg(id);

-- name: LockSubscriptionContract :one
SELECT id, contact_id, name, currency, starts_on, ends_on, mrr::text, version, created_by, created_at
FROM fyniq.subscription_contract WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: ListSubscriptionContracts :many
SELECT id, contact_id, name, currency, starts_on, ends_on, mrr::text, version, created_by, created_at
FROM fyniq.subscription_contract
WHERE id > sqlc.arg(after) AND (sqlc.arg(contact)::uuid = '00000000-0000-0000-0000-000000000000' OR contact_id = sqlc.arg(contact))
ORDER BY id LIMIT sqlc.arg(max);

-- name: SubscriptionAmendmentsOf :many
SELECT id, contract_id, kind, effective_on, mrr::text, reason, created_by, created_at
FROM fyniq.subscription_amendment WHERE contract_id = sqlc.arg(contract_id) ORDER BY effective_on, id;

-- name: AllSubscriptionAmendments :many
SELECT id, contract_id, kind, effective_on, mrr::text, reason, created_by, created_at
FROM fyniq.subscription_amendment ORDER BY contract_id, effective_on, id;

-- name: InsertSubscriptionAmendment :one
INSERT INTO fyniq.subscription_amendment (contract_id, kind, effective_on, mrr, reason, created_by, created_at)
VALUES (sqlc.arg(contract_id), sqlc.arg(kind), sqlc.arg(effective_on), sqlc.arg(mrr)::numeric, sqlc.arg(reason), sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING id, contract_id, kind, effective_on, mrr::text, reason, created_by, created_at;

-- name: BumpSubscriptionContract :one
UPDATE fyniq.subscription_contract SET version = version + 1 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING version;
