-- SOD-5 when a reconciliation is signed off (PRD-001 §6.3, §6.4, FR-BNK-10; rev 0.179, the owner's decisions of 6 Nov
-- 2026; p1-users-roles task 1.3).
--
-- Whoever decided a line of the money account in the period (categorised, matched, transferred, parked in suspense or
-- excluded it, as the line stands) recorded its bank transactions, so in Standard and Enterprise modes they sign off its
-- reconciliation only under an override: a request of their own, bank_reconciliation.sign, with a reason, that another
-- person approved (§6.4). Founder mode allows it, FM-5 and the preparer rule being its compensating controls. The
-- override is kept on the reconciliation and reported on Checks (C.2 #62). The database is the backstop (FY267):
--   - a signer who decided a line of the account in the period, in Standard or Enterprise mode, names the approved
--     request that holds exactly this reconciliation, the signer as its requester and the reason;
--   - an override is kept only with the sign-off it allowed, and only as that request holds it.
-- The widened and new checks are added NOT VALID and validated by 000232 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

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
                               ('bank_reconciliation', 'bank_reconciliation.sign'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset',
                  'payee_bank_change', 'claimant_bank_change', 'payment_batch', 'contact_member_link',
                  'director_fee', 'director_settlement', 'director_repayment', 'account', 'bank_reconciliation')) NOT VALID;

ALTER TABLE fyniq.bank_reconciliation ADD COLUMN sod_override_approval uuid;
ALTER TABLE fyniq.bank_reconciliation ADD COLUMN sod_override_reason text;
ALTER TABLE fyniq.bank_reconciliation ADD CONSTRAINT bank_reconciliation_override_approval_fkey
  FOREIGN KEY (organisation_id, sod_override_approval) REFERENCES fyniq.approval_request (organisation_id, id) NOT VALID;
ALTER TABLE fyniq.bank_reconciliation ADD CONSTRAINT bank_reconciliation_override_whole CHECK (
  (sod_override_approval IS NULL) = (sod_override_reason IS NULL)
  AND (sod_override_approval IS NULL OR signed_by IS NOT NULL)) NOT VALID;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.bank_reconciliation_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  closed  boolean;
  cur     text;
  mode    text;
  decided boolean;
  r       fyniq.approval_request;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A reconciliation is never deleted' USING ERRCODE = 'FY226';
  END IF;
  SELECT status = 'closed' INTO closed FROM fyniq.period WHERE organisation_id = NEW.organisation_id AND id = NEW.period_id;
  IF coalesce(closed, false) THEN
    RAISE EXCEPTION 'The reconciliation of a closed period does not change' USING ERRCODE = 'FY226',
      HINT = 'It is locked with its period (FR-BNK-10).';
  END IF;
  IF TG_OP = 'INSERT' THEN
    SELECT currency INTO cur FROM fyniq.money_account WHERE organisation_id = NEW.organisation_id AND id = NEW.money_account_id;
    IF NEW.currency <> cur OR NEW.signed_by IS NOT NULL OR NEW.version <> 1 OR NEW.sod_override_approval IS NOT NULL THEN
      RAISE EXCEPTION 'A reconciliation starts unsigned, at version 1, in its account''s currency' USING ERRCODE = 'FY226';
    END IF;
    RETURN NEW;
  END IF;
  IF OLD.signed_by IS NOT NULL OR NEW.version <> OLD.version + 1
     OR (NEW.money_account_id, NEW.period_id, NEW.currency) IS DISTINCT FROM (OLD.money_account_id, OLD.period_id, OLD.currency) THEN
    RAISE EXCEPTION 'Reconciliation % is signed, or changes one version at a time for its account and period', OLD.id USING ERRCODE = 'FY226';
  END IF;
  IF NEW.signed_by IS NOT NULL THEN
    IF NEW.signed_by = NEW.prepared_by THEN
      RAISE EXCEPTION 'The preparer of reconciliation % cannot sign it off', OLD.id USING ERRCODE = 'FY226',
        HINT = 'Another person with the sign-off right signs it (FM-5, SOD-5).';
    END IF;
    IF (NEW.statement_balance, NEW.book_balance, NEW.unreconciled_total, NEW.unreconciled_count, NEW.difference, NEW.ledger_revision,
        NEW.prepared_by, NEW.prepared_at)
       IS DISTINCT FROM (OLD.statement_balance, OLD.book_balance, OLD.unreconciled_total, OLD.unreconciled_count, OLD.difference,
        OLD.ledger_revision, OLD.prepared_by, OLD.prepared_at) THEN
      RAISE EXCEPTION 'Reconciliation % is signed as it was prepared', OLD.id USING ERRCODE = 'FY226';
    END IF;
    -- SOD-5: the signer decided none of the account's lines in the period, or the team mode allows it, or an approved
    -- override of theirs holds exactly this sign-off.
    SELECT m.mode INTO mode FROM fyniq.team_mode m WHERE m.organisation_id = NEW.organisation_id ORDER BY m.version DESC LIMIT 1;
    SELECT EXISTS (
      SELECT 1 FROM fyniq.bank_line l JOIN fyniq.period p ON p.organisation_id = l.organisation_id AND p.id = NEW.period_id
       WHERE l.organisation_id = NEW.organisation_id AND l.money_account_id = NEW.money_account_id
         AND l.booking_date BETWEEN p.starts_on AND p.ends_on
         AND l.status IN ('categorised', 'matched', 'transferred', 'suspense', 'excluded') AND l.status_by = NEW.signed_by)
      INTO decided;
    IF NEW.sod_override_approval IS NOT NULL THEN
      SELECT * INTO r FROM fyniq.approval_request q WHERE q.organisation_id = NEW.organisation_id AND q.id = NEW.sod_override_approval;
      IF r.status IS DISTINCT FROM 'approved' OR r.object_type <> 'bank_reconciliation' OR r.operation <> 'bank_reconciliation.sign'
         OR r.object_key <> NEW.id::text OR 'user:' || r.requested_by::text <> NEW.signed_by
         OR (r.change -> 'execute' ->> 'reason') IS DISTINCT FROM NEW.sod_override_reason THEN
        RAISE EXCEPTION 'The override on reconciliation % is not the approved request that holds exactly it', OLD.id
          USING ERRCODE = 'FY267', HINT = 'The signer asks for the override with a reason, and another person approves it (SOD-5, §6.4).';
      END IF;
    ELSIF decided AND coalesce(mode, 'founder') IN ('standard', 'enterprise') THEN
      RAISE EXCEPTION 'The signer of reconciliation % decided lines of its account in the period, which % mode blocks', OLD.id, mode
        USING ERRCODE = 'FY267', HINT = 'Another person signs it off, or the signer asks for an override that another person approves (SOD-5).';
    END IF;
  ELSIF NEW.sod_override_approval IS NOT NULL THEN
    RAISE EXCEPTION 'An override on reconciliation % comes only with its sign-off', OLD.id USING ERRCODE = 'FY267';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT UPDATE (sod_override_approval, sod_override_reason) ON fyniq.bank_reconciliation TO fyniq_api;
