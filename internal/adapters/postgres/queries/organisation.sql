-- name: NewOrganisationID :one
SELECT uuidv7()::uuid AS id;

-- name: SetTenant :exec
-- The first statement of every unit of work (SEC-02): transaction-scoped, so a pooled connection
-- never carries one tenant into the next transaction.
SELECT set_config('app.org_id', sqlc.arg(org_id)::text, true), set_config('lock_timeout', sqlc.arg(lock_timeout)::text, true);

-- name: InsertOrganisation :one
INSERT INTO fyniq.organisation (name, base_currency, base_minor_units)
SELECT sqlc.arg(name)::text, c.code, c.minor_units FROM fyniq.currencies c WHERE c.code = sqlc.arg(base_currency)::text
RETURNING organisation_id, name, base_currency, base_minor_units, version;

-- name: GetOrganisation :one
SELECT organisation_id, name, base_currency, base_minor_units, version FROM fyniq.organisation;
