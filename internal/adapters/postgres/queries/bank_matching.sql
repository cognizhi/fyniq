-- Bank rules, matches, transfers, signals and reconciliations (p1-bank-import-reconcile task 2.2).

-- name: InsertBankRule :one
INSERT INTO fyniq.bank_rule (name, position, conditions, action, active, from_ai_proposal, created_by, created_at, changed_by, changed_at)
VALUES (sqlc.arg(name), sqlc.arg(position), sqlc.arg(conditions), sqlc.arg(action), sqlc.arg(active), sqlc.arg(from_ai_proposal),
  sqlc.arg(by), sqlc.arg(at), sqlc.arg(by), sqlc.arg(at))
RETURNING *;

-- name: UpdateBankRule :one
UPDATE fyniq.bank_rule
   SET name = sqlc.arg(name), position = sqlc.arg(position), conditions = sqlc.arg(conditions), action = sqlc.arg(action),
       active = sqlc.arg(active), changed_by = sqlc.arg(by), changed_at = sqlc.arg(at), version = version + 1
 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: SetBankRuleAutoApply :one
UPDATE fyniq.bank_rule
   SET auto_apply = true, auto_apply_approval_id = sqlc.arg(approval_id), changed_by = sqlc.arg(by), changed_at = sqlc.arg(at),
       version = version + 1
 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: BankRules :many
SELECT * FROM fyniq.bank_rule ORDER BY position, created_at, id;

-- name: BankRule :one
SELECT * FROM fyniq.bank_rule WHERE id = sqlc.arg(id);

-- name: InsertBankMatch :one
INSERT INTO fyniq.bank_match (line_id, kind, document_id, entry_id, transfer_id, basis, rule_id, made_by, made_at)
VALUES (sqlc.arg(line_id), sqlc.arg(kind), sqlc.narg(document_id), sqlc.narg(entry_id), sqlc.narg(transfer_id), sqlc.arg(basis),
  sqlc.narg(rule_id), sqlc.arg(by), sqlc.arg(at))
RETURNING *;

-- name: UndoBankMatch :one
UPDATE fyniq.bank_match SET undone_by = sqlc.arg(by), undone_at = sqlc.arg(at), undo_reason = sqlc.arg(reason)
 WHERE id = sqlc.arg(id) AND undone_at IS NULL
RETURNING *;

-- name: BankMatches :many
SELECT * FROM fyniq.bank_match
 WHERE (sqlc.narg(line_id)::uuid IS NULL OR line_id = sqlc.narg(line_id)::uuid)
   AND (sqlc.narg(document_id)::uuid IS NULL OR document_id = sqlc.narg(document_id)::uuid)
   AND (NOT sqlc.arg(system_only)::boolean OR basis <> 'person')
   AND (sqlc.narg(since)::timestamptz IS NULL OR made_at >= sqlc.narg(since)::timestamptz)
   AND (sqlc.arg(all_matches)::boolean OR undone_at IS NULL)
 ORDER BY made_at DESC, id DESC;

-- name: InsertBankTransfer :one
INSERT INTO fyniq.bank_transfer (from_account_id, to_account_id, from_line_id, to_line_id, on_date, from_currency, from_minor_units,
  from_amount, to_currency, to_minor_units, to_amount, fee, entry_id, made_by, made_at)
SELECT sqlc.arg(from_account_id), sqlc.arg(to_account_id), sqlc.narg(from_line_id), sqlc.narg(to_line_id), sqlc.arg(on_date),
  f.code, f.minor_units, sqlc.arg(from_amount)::numeric, t.code, t.minor_units, sqlc.arg(to_amount)::numeric, sqlc.narg(fee)::numeric,
  sqlc.narg(entry_id), sqlc.arg(by), sqlc.arg(at)
  FROM fyniq.currencies f, fyniq.currencies t WHERE f.code = sqlc.arg(from_currency) AND t.code = sqlc.arg(to_currency)
RETURNING *;

-- name: CompleteBankTransfer :one
UPDATE fyniq.bank_transfer
   SET from_line_id = coalesce(from_line_id, sqlc.narg(from_line_id)), to_line_id = coalesce(to_line_id, sqlc.narg(to_line_id)),
       entry_id = coalesce(entry_id, sqlc.narg(entry_id)), version = version + 1
 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: UndoBankTransfer :one
UPDATE fyniq.bank_transfer
   SET undone_by = sqlc.arg(undone_by), undone_at = sqlc.arg(undone_at), undo_reason = sqlc.arg(undo_reason), version = version + 1
 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: BankTransfer :one
SELECT * FROM fyniq.bank_transfer WHERE id = sqlc.arg(id);

-- name: BankTransfersInTransit :many
-- Transfers with a side not yet matched, leaving out those undone.
SELECT * FROM fyniq.bank_transfer WHERE (from_line_id IS NULL OR to_line_id IS NULL) AND undone_at IS NULL ORDER BY on_date, id;

-- name: InsertBankSignal :one
INSERT INTO fyniq.bank_signal (money_account_id, amount, currency, minor_units, payer, reference, received_at, inbound_message_id, invoice_id)
SELECT sqlc.arg(money_account_id), sqlc.arg(amount)::numeric, c.code, c.minor_units, sqlc.narg(payer), sqlc.narg(reference),
  sqlc.arg(received_at), sqlc.narg(inbound_message_id), sqlc.narg(invoice_id)
  FROM fyniq.currencies c WHERE c.code = sqlc.arg(currency)
RETURNING *;

-- name: CloseBankSignal :one
UPDATE fyniq.bank_signal SET status = sqlc.arg(status), closed_line_id = sqlc.narg(closed_line_id), closed_at = sqlc.arg(at)
 WHERE id = sqlc.arg(id) AND status = 'pending'
RETURNING *;

-- name: PendingBankSignals :many
SELECT * FROM fyniq.bank_signal WHERE status = 'pending' ORDER BY received_at, id;

-- name: BankSignals :many
SELECT * FROM fyniq.bank_signal ORDER BY received_at DESC, id DESC;

-- name: BankSignal :one
SELECT * FROM fyniq.bank_signal WHERE id = sqlc.arg(id);

-- name: InsertBankReconciliation :one
INSERT INTO fyniq.bank_reconciliation (money_account_id, period_id, currency, minor_units, statement_balance, book_balance,
  unreconciled_total, unreconciled_count, difference, ledger_revision, prepared_by, prepared_at)
SELECT sqlc.arg(money_account_id), sqlc.arg(period_id), c.code, c.minor_units, sqlc.arg(statement_balance)::numeric,
  sqlc.arg(book_balance)::numeric, sqlc.arg(unreconciled_total)::numeric, sqlc.arg(unreconciled_count), sqlc.arg(difference)::numeric,
  sqlc.arg(ledger_revision), sqlc.arg(by), sqlc.arg(at)
  FROM fyniq.currencies c WHERE c.code = sqlc.arg(currency)
RETURNING *;

-- name: ReprepareBankReconciliation :one
UPDATE fyniq.bank_reconciliation
   SET statement_balance = sqlc.arg(statement_balance)::numeric, book_balance = sqlc.arg(book_balance)::numeric,
       unreconciled_total = sqlc.arg(unreconciled_total)::numeric, unreconciled_count = sqlc.arg(unreconciled_count),
       difference = sqlc.arg(difference)::numeric, ledger_revision = sqlc.arg(ledger_revision), prepared_by = sqlc.arg(by),
       prepared_at = sqlc.arg(at), version = version + 1
 WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SignBankReconciliation :one
UPDATE fyniq.bank_reconciliation
   SET signed_by = sqlc.arg(by), signed_at = sqlc.arg(at), signer_mfa_at = sqlc.arg(mfa_at), version = version + 1
 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: BankReconciliations :many
SELECT * FROM fyniq.bank_reconciliation WHERE period_id = sqlc.arg(period_id) ORDER BY money_account_id;

-- name: BankReconciliation :one
SELECT * FROM fyniq.bank_reconciliation WHERE money_account_id = sqlc.arg(money_account_id) AND period_id = sqlc.arg(period_id);
