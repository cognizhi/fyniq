-- Directors' settlements: a loan kept or a waiver (PRD-001 FR-EQ-05, FR-EQ-07, FM-9; p1-founders-money tasks 4.1 and
-- 4.2, design D10).
--
-- A settlement of what the company owes a director or shareholder is a director_settlement document, numbered DS- when it
-- is decided: a draft takes the loan note, minute or deed as its evidence, and the approval of director_settlement.approve
-- decides it, keeping the loan on its terms with no entry or posting the waiver (Dr 2500/2120 naming the person · Cr 3300
-- or income). The database is the backstop: a settlement is posted only under an approved director_settlement.approve
-- request that neither its requester nor the member who signs in as the person settled signed (FY253; FM-9, in every
-- team mode). The widened checks are added NOT VALID and validated by 000213 (squawk: constraint-missing-not-valid).

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
                  'director_confirmation', 'director_settlement')) NOT VALID;
ALTER TABLE fyniq.document_sequence DROP CONSTRAINT document_sequence_type_known;
ALTER TABLE fyniq.document_sequence ADD CONSTRAINT document_sequence_type_known
  CHECK (document_type IN ('journal', 'bill', 'bill_payment', 'vendor_credit', 'supplier_refund', 'purchase_order', 'invoice',
                           'customer_receipt', 'write_off', 'credit_note', 'customer_refund', 'customer_deposit', 'quote',
                           'sales_order', 'statement', 'expense_claim', 'claim_reimbursement', 'cash_advance', 'fixed_asset',
                           'asset_disposal', 'payment_batch', 'pre_incorporation_cost', 'director_fee',
                           'director_settlement')) NOT VALID;

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
                               ('director_settlement', 'director_settlement.approve'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset',
                  'payee_bank_change', 'claimant_bank_change', 'payment_batch', 'contact_member_link',
                  'director_fee', 'director_settlement')) NOT VALID;

-- +goose StatementBegin
-- A settlement is posted only under an approved director_settlement.approve request that neither its requester nor the
-- member who signs in as the person settled signed (FY253; FR-EQ-05, FM-9).
CREATE FUNCTION fyniq.director_settlement_approved() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  settled uuid;
BEGIN
  IF TG_OP = 'UPDATE' AND OLD.status = 'posted' THEN
    RETURN NULL;
  END IF;
  SELECT c.member_user_id INTO settled FROM fyniq.document_version v
    JOIN fyniq.contact c ON c.organisation_id = v.organisation_id AND c.id::text = v.payload->>'contact_id'
   WHERE v.organisation_id = NEW.organisation_id AND v.document_id = NEW.id AND v.version = NEW.current_version;
  IF NOT EXISTS (
       SELECT 1 FROM fyniq.approval_request r
        WHERE r.organisation_id = NEW.organisation_id AND r.object_type = 'director_settlement' AND r.operation = 'director_settlement.approve'
          AND r.object_key = NEW.id::text AND r.status = 'approved'
          AND NOT EXISTS (SELECT 1 FROM fyniq.approval_signature s
                           WHERE s.organisation_id = r.organisation_id AND s.request_id = r.id
                             AND (s.signer = r.requested_by OR s.signer = settled))) THEN
    RAISE EXCEPTION 'Settlement % is decided only on another person''s approval, never the person''s own', NEW.id
      USING ERRCODE = 'FY253', HINT = 'Ask for approval: someone who did not ask for it, and is not the person settled, approves it (FR-EQ-05, FM-9).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER director_settlement_approved AFTER INSERT OR UPDATE OF status ON fyniq.document
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.type = 'director_settlement' AND NEW.status = 'posted')
  EXECUTE FUNCTION fyniq.director_settlement_approved();
