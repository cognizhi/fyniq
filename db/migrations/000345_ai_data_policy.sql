-- Where AI connections may send which data (PRD-001 AI-75, AI-83, AI-88, CMP-PD-06; p1-ai-providers task 3.2, design D5).
--
-- Each connection declares its data handling: per model, endpoint and feature, where prompts are processed and stored,
-- their retention and training use, sub-processors and gateways, each with a dated official source or a contract term,
-- with its data inventory row and, when it processes outside Singapore, its recorded transfer. It is part of the
-- connection, so it changes as the connection does: asked for, approved by the Owner, and kept in each version.
--
-- The data-class policy maps the classes to the connections allowed beyond the defaults (personal data and payroll only
-- where Singapore processing is evidenced; restricted data nowhere). Each version is asked for and approved as the
-- approval object ai_data_policy, and kept as written: the next version only, never changed or removed (FY331). The
-- checks are added NOT VALID and 000346 validates them (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_connection ADD COLUMN data_handling jsonb NOT NULL DEFAULT '{}';
ALTER TABLE fyniq.ai_connection ADD CONSTRAINT ai_connection_data_handling_object CHECK (jsonb_typeof(data_handling) = 'object') NOT VALID;
GRANT INSERT (data_handling), UPDATE (data_handling) ON fyniq.ai_connection TO fyniq_api;

CREATE TABLE fyniq.ai_data_policy (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  version         integer     NOT NULL,
  policy          jsonb       NOT NULL,
  approval_id     uuid        NOT NULL,
  made_by         text        NOT NULL,
  made_at         timestamptz NOT NULL,
  CONSTRAINT ai_data_policy_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_data_policy_version UNIQUE (organisation_id, version),
  CONSTRAINT ai_data_policy_version_positive CHECK (version >= 1),
  CONSTRAINT ai_data_policy_object CHECK (jsonb_typeof(policy) = 'object'),
  -- Restricted data never goes to an AI provider, whatever a policy says (AI-02, AI-75).
  CONSTRAINT ai_data_policy_no_restricted CHECK (NOT (policy -> 'allow' ? 'restricted') OR jsonb_array_length(policy -> 'allow' -> 'restricted') = 0),
  CONSTRAINT ai_data_policy_made_by_format CHECK (made_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_data_policy_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A data-class policy''s version is kept as it was approved' USING ERRCODE = 'FY331';
  END IF;
  IF NEW.version <> 1 + coalesce((SELECT max(p.version) FROM fyniq.ai_data_policy p WHERE p.organisation_id = NEW.organisation_id), 0) THEN
    RAISE EXCEPTION 'A data-class policy''s next version follows its latest' USING ERRCODE = 'FY331';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER ai_data_policy_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.ai_data_policy
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_data_policy_guard();
CREATE TRIGGER ai_data_policy_no_truncate BEFORE TRUNCATE ON fyniq.ai_data_policy
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.ai_data_policy
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('ai_data_policy', 'id');

ALTER TABLE fyniq.ai_data_policy ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_data_policy FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_data_policy
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.ai_data_policy FROM PUBLIC;
GRANT SELECT ON fyniq.ai_data_policy TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (version, policy, approval_id, made_by, made_at) ON fyniq.ai_data_policy TO fyniq_api;

-- The approval requests of a data-class policy, under Appendix D's "AI providers and model routes".
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
                               ('ai_data_policy', 'ai_data_policy.change'))) NOT VALID;

ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset',
                  'payee_bank_change', 'claimant_bank_change', 'payment_batch', 'contact_member_link',
                  'director_fee', 'director_settlement', 'director_repayment', 'account', 'bank_reconciliation',
                  'holder_record', 'share_class', 'equity_transaction', 'instrument', 'instrument_template',
                  'option_pool', 'option_grant', 'option_event', 'round_scenario', 'investor_right', 'controller_entry', 'budget',
                  'migration_commit', 'tenant_export', 'ai_connection', 'ai_data_policy')) NOT VALID;
