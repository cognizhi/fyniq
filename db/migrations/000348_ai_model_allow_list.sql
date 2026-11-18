-- Each AI connection's allow-list of models (PRD-001 AI-72, AI-80, AI-81; p1-ai-providers task 4.1, design D3).
--
-- A listed model serves a connection only once it is on the connection's allow-list. It joins only after a probe on
-- that connection succeeded (a few tokens of synthetic text: a list says what exists, not what this credential may
-- call) and as an approval request of the operation ai_connection.allow_model, which the Owner approves (golden rule 6);
-- it leaves at once when a person takes it off, and by itself when a complete read no longer lists it, its shutdown
-- date passes, or the provider refuses it (AI-81). The database refuses an allowed model that is unprobed, unlisted or
-- not callable. Automatic qualification of newly listed models is a connection setting, off by default (AI-81). The
-- checks are added NOT VALID and 000349 validates them (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_model ADD COLUMN allowed boolean NOT NULL DEFAULT false;
ALTER TABLE fyniq.ai_model ADD COLUMN probed_at timestamptz;
ALTER TABLE fyniq.ai_model ADD COLUMN probe_ok boolean;
ALTER TABLE fyniq.ai_model ADD COLUMN probe_detail text;
ALTER TABLE fyniq.ai_model ADD CONSTRAINT ai_model_allowed_probed
  CHECK (NOT allowed OR (coalesce(probe_ok, false) AND listed AND not_callable_at IS NULL)) NOT VALID;
ALTER TABLE fyniq.ai_model ADD CONSTRAINT ai_model_probe_together CHECK ((probed_at IS NULL) = (probe_ok IS NULL)) NOT VALID;
ALTER TABLE fyniq.ai_model ADD CONSTRAINT ai_model_probe_detail_length CHECK (length(probe_detail) <= 500) NOT VALID;
GRANT INSERT (allowed), UPDATE (allowed, probed_at, probe_ok, probe_detail) ON fyniq.ai_model TO fyniq_api;
-- A read takes a model off the allow-list by itself; it never adds one, as only the API probes.
GRANT INSERT (allowed), UPDATE (allowed) ON fyniq.ai_model TO fyniq_worker;

ALTER TABLE fyniq.ai_connection ADD COLUMN auto_qualify boolean NOT NULL DEFAULT false;
GRANT INSERT (auto_qualify), UPDATE (auto_qualify) ON fyniq.ai_connection TO fyniq_api;

-- Adding a model is a request of the connection, under "AI providers and model routes".
ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_object_known CHECK (
  (object_type, operation) IN (('organisation_setting', 'organisation_setting.change'), ('membership', 'role.assign'),
                               ('membership', 'role.revoke'), ('membership', 'member.remove'),
                               ('member_invitation', 'member_invitation.create'),
                               ('approval_policy', 'approval_policy.change'), ('statutory_pack', 'statutory_pack.activate'),
                               ('team_mode', 'team_mode.change'), ('period', 'period.reopen'),
                               ('journal', 'journal.post'), ('journal', 'journal.reverse'), ('journal', 'journal.edit'),
                               ('opening_balance', 'opening_balance.edit'),
                               ('bill', 'bill.post'), ('bill', 'bill.void'), ('bill', 'bill.edit'),
                               ('bill_payment', 'bill_payment.void'),
                               ('credit_application', 'credit_application.void'),
                               ('supplier_refund', 'supplier_refund.void'),
                               ('purchase_order', 'purchase_order.approve'),
                               ('document_template', 'document_template.publish'),
                               ('invoice', 'invoice.post'), ('invoice', 'invoice.void'), ('invoice', 'invoice.edit'),
                               ('customer_receipt', 'customer_receipt.void'),
                               ('write_off', 'write_off.post'), ('write_off', 'write_off.void'),
                               ('customer_credit_application', 'customer_credit_application.void'),
                               ('customer_refund', 'customer_refund.void'),
                               ('customer_deposit', 'customer_deposit.void'),
                               ('recurring_invoice', 'recurring_invoice.approve'),
                               ('connector', 'connector.change'), ('bank_rule', 'bank_rule.auto_apply'),
                               ('expense_claim', 'expense_claim.post'), ('claim_reimbursement', 'claim_reimbursement.void'),
                               ('cash_advance', 'cash_advance.void'), ('asset_disposal', 'asset_disposal.void'),
                               ('fixed_asset', 'fixed_asset.void'),
                               ('payee_bank_change', 'payee_bank_change.approve'),
                               ('claimant_bank_change', 'claimant_bank_change.approve'),
                               ('payment_batch', 'payment_batch.approve'), ('payment_batch', 'payment_batch.reapprove'),
                               ('contact_member_link', 'contact_member_link.change'),
                               ('director_fee', 'director_fee.post'),
                               ('director_settlement', 'director_settlement.approve'),
                               ('director_repayment', 'director_repayment.void'),
                               ('account', 'account.merge'),
                               ('bank_reconciliation', 'bank_reconciliation.sign'),
                               ('holder_record', 'holder_record.record'), ('share_class', 'share_class.record'),
                               ('equity_transaction', 'equity_transaction.approve'),
                               ('instrument', 'instrument.record'),
                               ('instrument_template', 'instrument_template.enable'),
                               ('option_pool', 'option_pool.record'), ('option_grant', 'option_grant.record'),
                               ('option_event', 'option_event.record'), ('round_scenario', 'round_scenario.approve'),
                               ('investor_right', 'investor_right.record'),
                               ('controller_entry', 'controller_entry.record'),
                               ('budget', 'budget.record'),
                               ('migration_commit', 'migration.commit'),
                               ('tenant_export', 'tenant_export.prepare'),
                               ('ai_connection', 'ai_connection.create'), ('ai_connection', 'ai_connection.change'),
                               ('ai_connection', 'ai_connection.credential'), ('ai_connection', 'ai_connection.enable'),
                               ('ai_data_policy', 'ai_data_policy.change'),
                               ('ai_connection', 'ai_connection.allow_model'))) NOT VALID;
