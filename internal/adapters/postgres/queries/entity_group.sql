-- name: InsertGroupEntity :one
-- The parent's invitation row: label, who and when. Its state changes only through the database's functions (001350).
INSERT INTO fyniq.group_entity (label, invited_by, invited_at) VALUES (sqlc.arg(label), sqlc.arg(invited_by), sqlc.arg(invited_at))
RETURNING id, label, entity_org, entity_name, entity_currency, state, invited_by, invited_at, linked_by, linked_at, ended_by, ended_at, ended_reason, version;

-- name: ListGroupEntities :many
SELECT id, label, entity_org, entity_name, entity_currency, state, invited_by, invited_at, linked_by, linked_at, ended_by, ended_at, ended_reason, version
FROM fyniq.group_entity ORDER BY invited_at, id;

-- name: GetGroupEntity :one
SELECT id, label, entity_org, entity_name, entity_currency, state, invited_by, invited_at, linked_by, linked_at, ended_by, ended_at, ended_reason, version
FROM fyniq.group_entity WHERE id = sqlc.arg(id);

-- name: GetLiveGroupEntityOf :one
SELECT id, label, entity_org, entity_name, entity_currency, state, invited_by, invited_at, linked_by, linked_at, ended_by, ended_at, ended_reason, version
FROM fyniq.group_entity WHERE entity_org = sqlc.arg(entity_org) AND state = 'active';

-- name: ListGroupMemberships :many
SELECT id, parent_org, parent_name, parent_row, scopes, state, accepted_by, accepted_at, ended_by, ended_at, ended_reason, version
FROM fyniq.group_membership ORDER BY accepted_at, id;

-- name: GetGroupMembership :one
SELECT id, parent_org, parent_name, parent_row, scopes, state, accepted_by, accepted_at, ended_by, ended_at, ended_reason, version
FROM fyniq.group_membership WHERE id = sqlc.arg(id);

-- name: RegisterGroupInvitation :one
-- The security-definer function that registers the code of an invitation row of this organisation: only its Owner, never an organisation that is an entity (001350).
SELECT fyniq.register_group_invitation(sqlc.arg(row_id)::uuid, sqlc.arg(code_hash)::bytea, sqlc.arg(expires)::timestamptz, sqlc.arg(user_id)::uuid, sqlc.arg(at)::timestamptz)::uuid AS invitation_id;

-- name: AcceptGroupInvitation :one
SELECT r.out_parent_org::uuid AS parent_org, r.out_parent_name::text AS parent_name, r.out_parent_row::uuid AS parent_row, r.out_membership::uuid AS membership_id,
  r.out_entity_name::text AS entity_name, r.out_entity_currency::text AS entity_currency
FROM fyniq.accept_group_invitation(sqlc.arg(code_hash)::bytea, sqlc.arg(user_id)::uuid, sqlc.arg(at)::timestamptz) r;

-- name: EndGroupLink :one
SELECT r.out_role::text AS role, r.out_entity_row::uuid AS entity_row, r.out_parent_row::uuid AS parent_row, r.out_membership::uuid AS membership_id
FROM fyniq.end_group_link(sqlc.arg(user_id)::uuid, sqlc.arg(other)::uuid, sqlc.arg(reason)::text, sqlc.arg(at)::timestamptz) r;

-- name: WithdrawGroupInvitation :one
SELECT fyniq.withdraw_group_invitation(sqlc.arg(row_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(reason)::text, sqlc.arg(at)::timestamptz)::boolean AS withdrawn;

-- name: GroupEntityFigures :many
-- The parent's read of a linked entity: per account and month, the net base amount (001350). Fixed columns; a live link and the entity's consent are checked by the function.
SELECT f.account_code::text AS account_code, f.account_name::text AS account_name, f.account_type::text AS account_type, f.base_currency::text AS base_currency,
  f.month::date AS month, f.net_base::numeric AS net_base
FROM fyniq.group_entity_figures(sqlc.arg(entity)::uuid, sqlc.arg(through)::date) f;

-- name: GroupEntityPeriods :many
SELECT f.period_month::date AS period_month, f.period_status::text AS period_status, f.ledger_revision::bigint AS ledger_revision
FROM fyniq.group_entity_periods(sqlc.arg(entity)::uuid, sqlc.arg(through)::date) f;
