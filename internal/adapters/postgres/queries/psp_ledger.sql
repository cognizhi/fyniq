-- name: PSPAccounts :many
SELECT money_account_id, connector_id, payout_account_id, fee_account_id, disputes_account_id, dispute_loss_account_id, revenue_account_id, swept_through, swept_at, sweep_error, version
  FROM fyniq.psp_account ORDER BY money_account_id;

-- name: PSPAccount :one
SELECT money_account_id, connector_id, payout_account_id, fee_account_id, disputes_account_id, dispute_loss_account_id, revenue_account_id, swept_through, swept_at, sweep_error, version
  FROM fyniq.psp_account WHERE money_account_id = sqlc.arg(money_account_id);

-- name: InsertPSPAccount :one
INSERT INTO fyniq.psp_account (money_account_id, connector_id, payout_account_id, fee_account_id, disputes_account_id, dispute_loss_account_id, revenue_account_id, created_by, created_at)
VALUES (sqlc.arg(money_account_id), sqlc.arg(connector_id), sqlc.narg(payout_account_id), sqlc.arg(fee_account_id), sqlc.narg(disputes_account_id), sqlc.narg(dispute_loss_account_id), sqlc.narg(revenue_account_id),
  sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING money_account_id, connector_id, payout_account_id, fee_account_id, disputes_account_id, dispute_loss_account_id, revenue_account_id, swept_through, swept_at, sweep_error, version;

-- name: UpdatePSPAccount :one
UPDATE fyniq.psp_account SET payout_account_id = sqlc.narg(payout_account_id), fee_account_id = sqlc.arg(fee_account_id),
  disputes_account_id = sqlc.narg(disputes_account_id), dispute_loss_account_id = sqlc.narg(dispute_loss_account_id), revenue_account_id = sqlc.narg(revenue_account_id), version = version + 1
WHERE money_account_id = sqlc.arg(money_account_id) AND version = sqlc.arg(version)
RETURNING money_account_id, connector_id, payout_account_id, fee_account_id, disputes_account_id, dispute_loss_account_id, revenue_account_id, swept_through, swept_at, sweep_error, version;

-- name: SweptPSPAccount :execrows
UPDATE fyniq.psp_account SET swept_through = COALESCE(sqlc.narg(swept_through)::date, swept_through),
  swept_at = CASE WHEN sqlc.narg(swept_through)::date IS NULL THEN swept_at ELSE sqlc.arg(swept_at)::timestamptz END,
  sweep_error = sqlc.narg(sweep_error)
WHERE money_account_id = sqlc.arg(money_account_id);

-- name: InsertPSPTxn :one
INSERT INTO fyniq.psp_txn (connector_id, txn_id, type, category, source_kind, source_id, currency, gross, fee, net, available_on, payout_id, provider_at, seen_at)
VALUES (sqlc.arg(connector_id), sqlc.arg(txn_id), sqlc.arg(type), sqlc.arg(category), sqlc.narg(source_kind), sqlc.narg(source_id), sqlc.arg(currency),
  sqlc.arg(gross)::numeric, sqlc.arg(fee)::numeric, sqlc.arg(net)::numeric, sqlc.narg(available_on), sqlc.narg(payout_id), sqlc.arg(provider_at), sqlc.arg(seen_at))
ON CONFLICT ON CONSTRAINT psp_txn_once DO NOTHING
RETURNING id, connector_id, txn_id, type, category, source_kind, source_id, currency, gross, fee, net, available_on, payout_id, provider_at, seen_at;

-- name: PSPTxn :one
SELECT id, connector_id, txn_id, type, category, source_kind, source_id, currency, gross, fee, net, available_on, payout_id, provider_at, seen_at
  FROM fyniq.psp_txn WHERE connector_id = sqlc.arg(connector_id) AND txn_id = sqlc.arg(txn_id);

-- name: PSPTxnOfSource :one
SELECT id, connector_id, txn_id, type, category, source_kind, source_id, currency, gross, fee, net, available_on, payout_id, provider_at, seen_at
  FROM fyniq.psp_txn WHERE connector_id = sqlc.arg(connector_id) AND source_kind = sqlc.arg(source_kind) AND source_id = sqlc.arg(source_id)
 ORDER BY provider_at, txn_id LIMIT 1;

-- name: PSPTxnsOfPayout :many
SELECT id, connector_id, txn_id, type, category, source_kind, source_id, currency, gross, fee, net, available_on, payout_id, provider_at, seen_at
  FROM fyniq.psp_txn WHERE connector_id = sqlc.arg(connector_id) AND payout_id = sqlc.arg(payout_id) ORDER BY provider_at, txn_id;

-- name: SettlePSPTxn :one
UPDATE fyniq.psp_txn SET payout_id = sqlc.arg(payout_id)
WHERE connector_id = sqlc.arg(connector_id) AND txn_id = sqlc.arg(txn_id) AND payout_id IS NULL
RETURNING id;

-- name: InsertPSPPayout :one
INSERT INTO fyniq.psp_payout (connector_id, payout_id, currency, amount, status, arrival_date, automatic, provider_at, seen_at)
VALUES (sqlc.arg(connector_id), sqlc.arg(payout_id), sqlc.arg(currency), sqlc.arg(amount)::numeric, sqlc.arg(status), sqlc.narg(arrival_date),
  sqlc.arg(automatic), sqlc.arg(provider_at), sqlc.arg(seen_at))
ON CONFLICT ON CONSTRAINT psp_payout_once DO NOTHING
RETURNING id, connector_id, payout_id, currency, amount, status, arrival_date, automatic, provider_at, seen_at;

-- name: UpdatePSPPayout :one
UPDATE fyniq.psp_payout SET status = sqlc.arg(status), arrival_date = sqlc.narg(arrival_date), automatic = sqlc.arg(automatic)
WHERE connector_id = sqlc.arg(connector_id) AND payout_id = sqlc.arg(payout_id)
  AND (status, arrival_date, automatic) IS DISTINCT FROM (sqlc.arg(status)::text, sqlc.narg(arrival_date)::date, sqlc.arg(automatic)::boolean)
RETURNING id, connector_id, payout_id, currency, amount, status, arrival_date, automatic, provider_at, seen_at;

-- name: PSPPayout :one
SELECT id, connector_id, payout_id, currency, amount, status, arrival_date, automatic, provider_at, seen_at
  FROM fyniq.psp_payout WHERE connector_id = sqlc.arg(connector_id) AND payout_id = sqlc.arg(payout_id);

-- name: PSPPayouts :many
SELECT id, connector_id, payout_id, currency, amount, status, arrival_date, automatic, provider_at, seen_at
  FROM fyniq.psp_payout WHERE connector_id = sqlc.arg(connector_id) ORDER BY provider_at DESC, payout_id DESC LIMIT sqlc.arg(max_rows)::int;

-- name: InsertPSPBalance :one
INSERT INTO fyniq.psp_balance (connector_id, currency, amount, taken_at)
VALUES (sqlc.arg(connector_id), sqlc.arg(currency), sqlc.arg(amount)::numeric, sqlc.arg(taken_at))
RETURNING id, connector_id, currency, amount, taken_at;

-- name: LatestPSPBalance :one
SELECT id, connector_id, currency, amount, taken_at FROM fyniq.psp_balance
 WHERE connector_id = sqlc.arg(connector_id) AND currency = sqlc.arg(currency) ORDER BY taken_at DESC, id DESC LIMIT 1;

-- name: PSPDisputeTxns :many
SELECT id, connector_id, txn_id, type, category, source_kind, source_id, currency, gross, fee, net, available_on, payout_id, provider_at, seen_at
  FROM fyniq.psp_txn WHERE connector_id = sqlc.arg(connector_id) AND category IN ('dispute', 'dispute_reversal') ORDER BY provider_at, txn_id;

-- name: InsertPSPDisputeLoss :one
INSERT INTO fyniq.psp_dispute_loss (connector_id, dispute_id, document_id, currency, amount, accepted_overstated_revenue, reason, created_by, created_at)
VALUES (sqlc.arg(connector_id), sqlc.arg(dispute_id), sqlc.arg(document_id), sqlc.arg(currency), sqlc.arg(amount)::numeric,
  sqlc.arg(accepted_overstated_revenue), sqlc.narg(reason), sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING id, connector_id, dispute_id, document_id, currency, amount, accepted_overstated_revenue, reason, created_by, created_at;

-- name: PSPDisputeLosses :many
SELECT id, connector_id, dispute_id, document_id, currency, amount, accepted_overstated_revenue, reason, created_by, created_at
  FROM fyniq.psp_dispute_loss WHERE connector_id = sqlc.arg(connector_id) ORDER BY created_at, dispute_id;

-- name: InsertPSPDisputeCredit :one
INSERT INTO fyniq.psp_dispute_credit (connector_id, dispute_id, credit_note_id, refund_id, currency, amount, created_by, created_at)
VALUES (sqlc.arg(connector_id), sqlc.arg(dispute_id), sqlc.arg(credit_note_id), sqlc.arg(refund_id), sqlc.arg(currency), sqlc.arg(amount)::numeric,
  sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING id, connector_id, dispute_id, credit_note_id, refund_id, currency, amount, created_by, created_at;

-- name: PSPDisputeCredits :many
SELECT id, connector_id, dispute_id, credit_note_id, refund_id, currency, amount, created_by, created_at
  FROM fyniq.psp_dispute_credit WHERE connector_id = sqlc.arg(connector_id) ORDER BY created_at, dispute_id;
