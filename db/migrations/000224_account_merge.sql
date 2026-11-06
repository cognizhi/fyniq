-- Account merges (PRD-001 FR-COA-04, Appendix B.8; rev 0.174, the owner's decision of 6 Nov 2026; p1-organisation-chart
-- task 3.2, p1-approval-engine task 4.1).
--
-- A merge moves an account's balance into another account by one entry, account_merge@1, recorded as an
-- account_merge document, once someone other than the one who asked approves account.merge on "Chart of accounts,
-- dimensions" (FM-1). The account then stays inactive for good and names the account it was merged into. Earlier
-- postings stay where they are. The database is the backstop (FY260):
--   - merged_into_id is set once, never changed or cleared, and only as the account becomes inactive;
--   - a merged account never becomes active again, so it takes no posting after its merge;
--   - it names another account of the organisation, of the same type, active, not a group and with no system role;
--   - the account merged is no system account, money account or control account;
--   - a balance-sheet account merged holds nothing in any currency or dimension set.
-- The widened checks are added NOT VALID and validated by 000225 (squawk: constraint-missing-not-valid).

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
                  'director_confirmation', 'director_settlement', 'director_repayment', 'account_merge')) NOT VALID;

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
                               ('account', 'account.merge'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset',
                  'payee_bank_change', 'claimant_bank_change', 'payment_batch', 'contact_member_link',
                  'director_fee', 'director_settlement', 'director_repayment', 'account')) NOT VALID;

ALTER TABLE fyniq.account ADD COLUMN merged_into_id uuid;
ALTER TABLE fyniq.account ADD CONSTRAINT account_merged_into_fkey FOREIGN KEY (organisation_id, merged_into_id)
  REFERENCES fyniq.account (organisation_id, id) NOT VALID;

-- +goose StatementBegin
CREATE FUNCTION fyniq.account_merge_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t fyniq.account;
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.merged_into_id IS NOT NULL THEN
      RAISE EXCEPTION 'A new account % cannot be merged already', NEW.code USING ERRCODE = 'FY260',
        HINT = 'Merge an account once it exists, with an approved merge (FR-COA-04).';
    END IF;
    RETURN NEW;
  END IF;
  IF OLD.merged_into_id IS NOT NULL THEN
    IF NEW.merged_into_id IS DISTINCT FROM OLD.merged_into_id OR NEW.is_active THEN
      RAISE EXCEPTION 'Account % is merged, so it stays inactive and merged into the same account', OLD.code
        USING ERRCODE = 'FY260', HINT = 'Post to the account it was merged into (FR-COA-04).';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.merged_into_id IS NULL THEN
    RETURN NEW;
  END IF;
  IF NEW.is_active THEN
    RAISE EXCEPTION 'Account % becomes inactive as it is merged', NEW.code USING ERRCODE = 'FY260';
  END IF;
  IF NEW.role IS NOT NULL OR NEW.is_money_account OR NEW.is_control OR NEW.is_group THEN
    RAISE EXCEPTION 'Account % is a system, money, control or group account, which is never merged', NEW.code
      USING ERRCODE = 'FY260', HINT = 'Deactivate it instead, or move its balance with a journal or a transfer.';
  END IF;
  SELECT * INTO t FROM fyniq.account WHERE organisation_id = NEW.organisation_id AND id = NEW.merged_into_id;
  IF NOT FOUND OR t.id = NEW.id OR t.type <> NEW.type OR NOT t.is_active OR t.is_group OR t.role IS NOT NULL THEN
    RAISE EXCEPTION 'Account % merges only into another active account of type % that is not a group or a system account',
      NEW.code, NEW.type USING ERRCODE = 'FY260';
  END IF;
  IF NEW.type IN ('asset', 'liability', 'equity') AND EXISTS (
       SELECT 1 FROM fyniq.balance b
        WHERE b.organisation_id = NEW.organisation_id AND b.account_id = NEW.id
        GROUP BY b.currency, b.dimension_set_id
       HAVING sum(b.amount) <> 0 OR sum(b.base_amount) <> 0) THEN
    RAISE EXCEPTION 'Account % still holds a balance, so it is not merged yet', NEW.code USING ERRCODE = 'FY260',
      HINT = 'Post the merge''s entry first, in the same transaction (Appendix B.8).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER account_merge_guard BEFORE INSERT OR UPDATE ON fyniq.account
  FOR EACH ROW EXECUTE FUNCTION fyniq.account_merge_guard();

GRANT UPDATE (merged_into_id) ON fyniq.account TO fyniq_api;
