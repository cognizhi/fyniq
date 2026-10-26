-- The banking store (p1-bank-import-reconcile task 2.1): money accounts, their sources, saved mappings, statements,
-- lines and gaps.

-- name: InsertMoneyAccount :one
INSERT INTO fyniq.money_account (account_id, kind, name, institution, identifier, currency, opened_on, cardholder_id, created_by,
  created_at, changed_by, changed_at)
VALUES (sqlc.arg(account_id), sqlc.arg(kind), sqlc.arg(name), sqlc.narg(institution), sqlc.narg(identifier), sqlc.arg(currency),
  sqlc.narg(opened_on), sqlc.narg(cardholder_id), sqlc.arg(by), sqlc.arg(at), sqlc.arg(by), sqlc.arg(at))
RETURNING id;

-- name: UpdateMoneyAccount :execrows
UPDATE fyniq.money_account
   SET kind = sqlc.arg(kind), name = sqlc.arg(name), institution = sqlc.narg(institution), identifier = sqlc.narg(identifier),
       opened_on = sqlc.narg(opened_on), closed_on = sqlc.narg(closed_on), cardholder_id = sqlc.narg(cardholder_id),
       changed_by = sqlc.arg(by), changed_at = sqlc.arg(at),
       version = version + 1
 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version);

-- name: MoneyAccount :one
SELECT * FROM fyniq.money_account WHERE id = sqlc.arg(id);

-- name: MoneyAccounts :many
SELECT * FROM fyniq.money_account ORDER BY name, id;

-- name: InsertMoneyAccountSource :one
INSERT INTO fyniq.money_account_source (money_account_id, kind, connector_id, senders, report_format, mapping_id, from_date, set_by, set_at)
VALUES (sqlc.arg(money_account_id), sqlc.arg(kind), sqlc.narg(connector_id), sqlc.arg(senders)::text[], sqlc.narg(report_format),
  sqlc.narg(mapping_id), sqlc.arg(from_date), sqlc.arg(by), sqlc.arg(at))
RETURNING *;

-- name: MoneyAccountSources :many
SELECT * FROM fyniq.money_account_source WHERE money_account_id = sqlc.arg(money_account_id) ORDER BY from_date;

-- name: InsertStatementMapping :one
INSERT INTO fyniq.statement_mapping (name, institution, format, spec, created_by, created_at, changed_by, changed_at)
VALUES (sqlc.arg(name), sqlc.narg(institution), sqlc.arg(format), sqlc.arg(spec), sqlc.arg(by), sqlc.arg(at), sqlc.arg(by), sqlc.arg(at))
RETURNING *;

-- name: UpdateStatementMapping :one
UPDATE fyniq.statement_mapping
   SET name = sqlc.arg(name), institution = sqlc.narg(institution), spec = sqlc.arg(spec), changed_by = sqlc.arg(by),
       changed_at = sqlc.arg(at), version = version + 1
 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: StatementMappings :many
SELECT * FROM fyniq.statement_mapping ORDER BY name, id;

-- name: InsertBankStatement :one
INSERT INTO fyniq.bank_statement (money_account_id, source, format, parser_version, period_from, period_to, currency, minor_units,
  opening_balance, closing_balance, line_count, file_ref, connector_id, imported_by, imported_at)
SELECT sqlc.arg(money_account_id), sqlc.arg(source), sqlc.arg(format), sqlc.arg(parser_version), sqlc.arg(period_from),
  sqlc.arg(period_to), c.code, c.minor_units, sqlc.narg(opening_balance)::numeric, sqlc.narg(closing_balance)::numeric,
  sqlc.arg(line_count), sqlc.narg(file_ref), sqlc.narg(connector_id), sqlc.arg(by), sqlc.arg(at)
  FROM fyniq.currencies c WHERE c.code = sqlc.arg(currency)
RETURNING *;

-- name: InsertBankLine :one
INSERT INTO fyniq.bank_line (statement_id, money_account_id, position, booking_date, value_date, amount, currency, minor_units,
  reference, counterparty, description, running_balance, fingerprint, status, duplicate_of, status_by, status_at)
SELECT sqlc.arg(statement_id), sqlc.arg(money_account_id), sqlc.arg(position), sqlc.arg(booking_date), sqlc.narg(value_date),
  sqlc.arg(amount)::numeric, c.code, c.minor_units, sqlc.narg(reference), sqlc.narg(counterparty), sqlc.narg(description),
  sqlc.narg(running_balance)::numeric, sqlc.arg(fingerprint), sqlc.arg(status), sqlc.narg(duplicate_of), sqlc.arg(by), sqlc.arg(at)
  FROM fyniq.currencies c WHERE c.code = sqlc.arg(currency)
RETURNING *;

-- name: InsertBankGap :one
INSERT INTO fyniq.bank_gap (money_account_id, statement_id, after_date, before_date, currency, minor_units, expected_opening,
  found_opening, found_at)
SELECT sqlc.arg(money_account_id), sqlc.arg(statement_id), sqlc.narg(after_date), sqlc.arg(before_date), c.code, c.minor_units,
  sqlc.narg(expected_opening)::numeric, sqlc.narg(found_opening)::numeric, sqlc.arg(at)
  FROM fyniq.currencies c WHERE c.code = sqlc.arg(currency)
RETURNING *;

-- name: BankStatements :many
SELECT * FROM fyniq.bank_statement WHERE money_account_id = sqlc.arg(money_account_id) ORDER BY period_to DESC, imported_at DESC, id DESC;

-- name: BankLinesByFingerprint :many
SELECT * FROM fyniq.bank_line WHERE fingerprint = ANY (sqlc.arg(fingerprints)::text[]) ORDER BY booking_date, id;

-- name: BankLines :many
SELECT * FROM fyniq.bank_line
 WHERE (sqlc.narg(money_account_id)::uuid IS NULL OR money_account_id = sqlc.narg(money_account_id)::uuid)
   AND (cardinality(sqlc.arg(statuses)::text[]) = 0 OR status = ANY (sqlc.arg(statuses)::text[]))
   AND (sqlc.narg(from_date)::date IS NULL OR booking_date >= sqlc.narg(from_date)::date)
   AND (sqlc.narg(to_date)::date IS NULL OR booking_date <= sqlc.narg(to_date)::date)
 ORDER BY booking_date, statement_id, position;

-- name: BankLine :one
SELECT * FROM fyniq.bank_line WHERE id = sqlc.arg(id);

-- name: SetBankLineStatus :one
UPDATE fyniq.bank_line
   SET status = sqlc.arg(status), status_by = sqlc.arg(by), status_at = sqlc.arg(at), version = version + 1
 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: BankGaps :many
SELECT * FROM fyniq.bank_gap
 WHERE money_account_id = sqlc.arg(money_account_id) AND (NOT sqlc.arg(open_only)::boolean OR resolved_at IS NULL)
 ORDER BY before_date, id;

-- name: ResolveBankGap :one
UPDATE fyniq.bank_gap SET resolved_by = sqlc.arg(by), resolved_at = sqlc.arg(at), resolution = sqlc.arg(resolution)
 WHERE id = sqlc.arg(id) AND resolved_at IS NULL
RETURNING *;
