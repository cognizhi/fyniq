-- Approving an expense claim (PRD-001 FR-AP-17, FR-APR-01; p1-expense-claims task 2.1, design D3; the owner's decision
-- of 26 Oct 2026, rev 0.147).
--
-- The approval engine covers the object expense_claim with the operation expense_claim.post: the claimant asks, one
-- person other than the claimant who holds A on "Expense claims" approves (and the Controller too, as a second step,
-- when a line is above its category's limit), and the last approval posts the claim. The database is the backstop:
-- a claim is posted only under an approved request for it that its claimant asked for and did not sign (FY233),
-- checked at commit, so the post and the approval go together. The widened checks are added NOT VALID and validated by
-- 000163 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_object_known CHECK (
  (object_type, operation) IN (('organisation_setting', 'organisation_setting.change'), ('membership', 'role.assign'),
                               ('membership', 'role.revoke'), ('member_invitation', 'member_invitation.create'),
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
                               ('expense_claim', 'expense_claim.post'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim')) NOT VALID;

-- +goose StatementBegin
-- An expense claim posts only under an approved request to post it, asked for by its claimant and signed by someone
-- else (FY233; FR-AP-17: nobody approves their own claim, in any team mode).
CREATE FUNCTION fyniq.expense_claim_approved() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  claimant text;
BEGIN
  IF TG_OP = 'UPDATE' AND OLD.status = 'posted' THEN
    RETURN NULL;
  END IF;
  SELECT v.payload->>'claimant_id' INTO claimant FROM fyniq.document_version v
   WHERE v.organisation_id = NEW.organisation_id AND v.document_id = NEW.id AND v.version = NEW.current_version;
  IF claimant IS NULL OR NOT EXISTS (
       SELECT 1 FROM fyniq.approval_request r
        WHERE r.organisation_id = NEW.organisation_id AND r.object_type = 'expense_claim' AND r.operation = 'expense_claim.post'
          AND r.object_key = NEW.id::text AND r.status = 'approved' AND r.requested_by::text = claimant
          AND NOT EXISTS (SELECT 1 FROM fyniq.approval_signature s
                           WHERE s.organisation_id = r.organisation_id AND s.request_id = r.id AND s.signer::text = claimant)) THEN
    RAISE EXCEPTION 'Expense claim % posts only when someone other than its claimant approves it', NEW.id USING ERRCODE = 'FY233',
      HINT = 'The claimant submits it; another person who holds the approval right approves it (FR-AP-17).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER expense_claim_approved AFTER INSERT OR UPDATE OF status ON fyniq.document
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.type = 'expense_claim' AND NEW.status = 'posted')
  EXECUTE FUNCTION fyniq.expense_claim_approved();
