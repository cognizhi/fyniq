-- Budgets (PRD-001 FR-PLN-01, C.1 #35; p1-planning slice 4, tasks 4.1 and 4.2, design D6).
--
-- A budget document is one version of a financial year's budget: monthly amounts by income and expense account and
-- dimension value, signed as the P&L shows them. A person who holds A on Appendix D's "Budgets, forecasts" row
-- approves it, never the one who asked (FM-1), and approving it records and locks it. It posts nothing.
--
-- The database is the backstop (FY301): a budget keeps its own keys, a name of 1 to 80 characters, its kind (original,
-- or revised naming the budget it revises), its year's first day, and at most 5,000 lines, each an account, its
-- dimension values and 1 to 24 monthly amounts as decimal strings. The widened checks are added NOT VALID and
-- validated by 000280.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation', 'bill', 'bill_payment', 'vendor_credit', 'credit_application',
                  'supplier_refund', 'purchase_order', 'invoice', 'customer_receipt', 'write_off', 'credit_note',
                  'customer_credit_application', 'customer_refund', 'customer_deposit', 'quote', 'sales_order', 'statement',
                  'bank_transaction', 'bank_agreement', 'expense_claim', 'claim_reimbursement', 'cash_advance', 'fixed_asset',
                  'asset_disposal', 'payee_bank_change', 'claimant_bank_change', 'payment_batch',
                  'pre_incorporation_cost', 'director_pay_choice', 'director_fee', 'exempt_status',
                  'director_confirmation', 'director_settlement', 'director_repayment', 'account_merge',
                  'owner_succession', 'holder_record', 'share_class', 'equity_transaction', 'instrument',
                  'instrument_template', 'option_pool', 'option_grant', 'option_event', 'round_scenario', 'investor_right',
                  'controller_entry', 'acra_extract', 'gst_forecast', 'statutory_filing', 'forecast', 'budget')) NOT VALID;

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
                               ('budget', 'budget.record'))) NOT VALID;

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
                  'option_pool', 'option_grant', 'option_event', 'round_scenario', 'investor_right', 'controller_entry', 'budget')) NOT VALID;

-- +goose StatementBegin
-- A budget keeps its rules whoever writes it (FY301).
CREATE FUNCTION fyniq.budget_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t   text;
  p   jsonb := NEW.payload;
  bad text;
  x   jsonb;
  uid constant text := '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';
BEGIN
  SELECT d.type INTO t FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id;
  IF t IS DISTINCT FROM 'budget' THEN
    RETURN NEW;
  END IF;
  IF NOT fyniq.forecast_keys(p, ARRAY['name', 'kind', 'financial_year', 'revises', 'lines'], ARRAY['name', 'kind', 'financial_year', 'lines']) THEN
    bad := 'its keys';
  ELSIF jsonb_typeof(p->'name') IS DISTINCT FROM 'string' OR char_length(p->>'name') NOT BETWEEN 1 AND 80 THEN
    bad := 'its name';
  ELSIF p->>'kind' IS DISTINCT FROM 'original' AND p->>'kind' IS DISTINCT FROM 'revised' THEN
    bad := 'its kind';
  ELSIF (p->>'kind' = 'revised') <> (p ? 'revises') OR (p ? 'revises' AND p->>'revises' !~ uid) THEN
    bad := 'the budget it revises';
  ELSIF jsonb_typeof(p->'financial_year') IS DISTINCT FROM 'string' OR p->>'financial_year' !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN
    bad := 'its year';
  ELSIF jsonb_typeof(p->'lines') IS DISTINCT FROM 'array' OR jsonb_array_length(p->'lines') > 5000 THEN
    bad := 'its lines';
  END IF;
  IF bad IS NULL THEN
    FOR x IN SELECT * FROM jsonb_array_elements(p->'lines') LOOP
      IF NOT fyniq.forecast_keys(x, ARRAY['account_id', 'dimension_value_ids', 'amounts'], ARRAY['account_id', 'amounts'])
         OR x->>'account_id' !~ uid
         OR (x ? 'dimension_value_ids' AND (jsonb_typeof(x->'dimension_value_ids') IS DISTINCT FROM 'array'
             OR EXISTS (SELECT 1 FROM jsonb_array_elements(x->'dimension_value_ids') v WHERE jsonb_typeof(v) IS DISTINCT FROM 'string' OR v #>> '{}' !~ uid)))
         OR jsonb_typeof(x->'amounts') IS DISTINCT FROM 'array' OR jsonb_array_length(x->'amounts') NOT BETWEEN 1 AND 24
         OR EXISTS (SELECT 1 FROM jsonb_array_elements(x->'amounts') a WHERE NOT fyniq.forecast_decimal(a, true)) THEN
        bad := 'a line';
        EXIT;
      END IF;
    END LOOP;
  END IF;
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'A budget keeps its own keys and decimal strings: % does not', bad USING ERRCODE = 'FY301',
      HINT = 'Send the budget as the API takes it: a name, original or revised, its year''s first day, and at most 5,000 lines of an account, its dimension values and a decimal amount for each month (FR-PLN-01).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER budget_guard BEFORE INSERT ON fyniq.document_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.budget_guard();
