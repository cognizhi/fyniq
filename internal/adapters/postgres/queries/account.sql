-- name: InsertAccount :one
INSERT INTO fyniq.account (code, name, type, parent_id, is_group, role, report_group, cash_flow_line, function, is_money_account,
  default_gst_code, currency, is_control, is_payroll_cost, guidance)
VALUES (sqlc.arg(code), sqlc.arg(name), sqlc.arg(type), sqlc.narg(parent_id), sqlc.arg(is_group), sqlc.narg(role),
  sqlc.narg(report_group), sqlc.narg(cash_flow_line), sqlc.narg(function), sqlc.arg(is_money_account), sqlc.narg(default_gst_code),
  sqlc.narg(currency), sqlc.arg(is_control), sqlc.arg(is_payroll_cost), sqlc.narg(guidance))
RETURNING id, code, name, type, parent_id, is_group, is_active, version, role, report_group, cash_flow_line, function, is_money_account, default_gst_code, currency, is_control, is_payroll_cost, guidance;

-- name: GetAccount :one
SELECT id, code, name, type, parent_id, is_group, is_active, version, role, report_group, cash_flow_line, function, is_money_account, default_gst_code, currency, is_control, is_payroll_cost, guidance FROM fyniq.account WHERE id = $1;

-- name: GetAccountByCode :one
SELECT id, code, name, type, parent_id, is_group, is_active, version, role, report_group, cash_flow_line, function, is_money_account, default_gst_code, currency, is_control, is_payroll_cost, guidance FROM fyniq.account WHERE code = $1;

-- name: SetAccountActive :one
UPDATE fyniq.account SET is_active = sqlc.arg(is_active), version = version + 1
WHERE id = sqlc.arg(id)
RETURNING id, code, name, type, parent_id, is_group, is_active, version, role, report_group, cash_flow_line, function, is_money_account, default_gst_code, currency, is_control, is_payroll_cost, guidance;

-- name: ListAccounts :many
SELECT id, code, name, type, parent_id, is_group, is_active, version, role, report_group, cash_flow_line, function, is_money_account, default_gst_code, currency, is_control, is_payroll_cost, guidance FROM fyniq.account ORDER BY code;

-- name: PageAccounts :many
SELECT id, code, name, type, parent_id, is_group, is_active, version, role, report_group, cash_flow_line, function, is_money_account, default_gst_code, currency, is_control, is_payroll_cost, guidance FROM fyniq.account
WHERE code > sqlc.arg(after)::text ORDER BY code LIMIT sqlc.arg(page_size);

-- name: CountAccounts :one
SELECT count(*) FROM fyniq.account;

-- name: LockAccount :one
SELECT id, code, name, type, parent_id, is_group, is_active, version, role, report_group, cash_flow_line, function, is_money_account, default_gst_code, currency, is_control, is_payroll_cost, guidance FROM fyniq.account WHERE id = $1 FOR UPDATE;

-- name: UpdateAccount :one
UPDATE fyniq.account SET name = sqlc.arg(name), parent_id = sqlc.narg(parent_id), is_group = sqlc.arg(is_group),
  is_active = sqlc.arg(is_active), report_group = sqlc.narg(report_group), cash_flow_line = sqlc.narg(cash_flow_line),
  function = sqlc.narg(function), is_money_account = sqlc.arg(is_money_account), default_gst_code = sqlc.narg(default_gst_code),
  currency = sqlc.narg(currency), is_control = sqlc.arg(is_control), is_payroll_cost = sqlc.arg(is_payroll_cost),
  guidance = sqlc.narg(guidance), version = version + 1
WHERE id = sqlc.arg(id)
RETURNING id, code, name, type, parent_id, is_group, is_active, version, role, report_group, cash_flow_line, function, is_money_account, default_gst_code, currency, is_control, is_payroll_cost, guidance;
