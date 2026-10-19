-- name: NewOrganisationID :one
SELECT uuidv7()::uuid AS id;

-- name: SetTenant :exec
-- The first statement of every unit of work (SEC-02): transaction-scoped, so a pooled connection
-- never carries one tenant into the next transaction.
SELECT set_config('app.org_id', sqlc.arg(org_id)::text, true), set_config('lock_timeout', sqlc.arg(lock_timeout)::text, true);

-- name: CheckDeferred :exec
-- A preview's last statement before it rolls back (FR-INT-14): the deferred checks (INV-01's balance,
-- the audit requirements) run now, as they would at commit.
SET CONSTRAINTS ALL IMMEDIATE;

-- name: InsertOrganisation :one
INSERT INTO fyniq.organisation (name, base_currency, base_minor_units)
SELECT sqlc.arg(name)::text, c.code, c.minor_units FROM fyniq.currencies c WHERE c.code = sqlc.arg(base_currency)::text
RETURNING organisation_id, name, base_currency, base_minor_units, version;

-- name: GetOrganisation :one
SELECT organisation_id, name, base_currency, base_minor_units, version, legal_name, uen, registered_address, mailing_address, email, phone, website, bank_name, bank_account_name, bank_account_number, paynow_uen, document_footer, fy_start_month, books_start, first_year_end, time_zone FROM fyniq.organisation;

-- name: LockOrganisation :one
-- The organisation's row, locked for a change of its profile (FR-INT-09: the version is compared under the lock).
SELECT organisation_id, name, base_currency, base_minor_units, version, legal_name, uen, registered_address, mailing_address, email, phone, website, bank_name, bank_account_name, bank_account_number, paynow_uen, document_footer, fy_start_month, books_start, first_year_end, time_zone FROM fyniq.organisation FOR UPDATE;

-- name: UpdateOrganisationProfile :one
UPDATE fyniq.organisation SET legal_name = sqlc.narg(legal_name), uen = sqlc.narg(uen),
  registered_address = sqlc.narg(registered_address), mailing_address = sqlc.narg(mailing_address), email = sqlc.narg(email),
  phone = sqlc.narg(phone), website = sqlc.narg(website), bank_name = sqlc.narg(bank_name),
  bank_account_name = sqlc.narg(bank_account_name), bank_account_number = sqlc.narg(bank_account_number),
  paynow_uen = sqlc.narg(paynow_uen), document_footer = sqlc.narg(document_footer), fy_start_month = sqlc.arg(fy_start_month),
  books_start = sqlc.narg(books_start), first_year_end = sqlc.narg(first_year_end), time_zone = sqlc.arg(time_zone),
  version = version + 1
RETURNING organisation_id, name, base_currency, base_minor_units, version, legal_name, uen, registered_address, mailing_address, email, phone, website, bank_name, bank_account_name, bank_account_number, paynow_uen, document_footer, fy_start_month, books_start, first_year_end, time_zone;
