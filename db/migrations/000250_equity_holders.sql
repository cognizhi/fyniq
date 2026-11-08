-- Holders and share classes (PRD-001 FR-EQ-15, FR-EQ-16; rev 0.203; p1-equity-register slice 1, design D1 and D2).
--
-- A holder is a contact marked shareholder, or marked investor (a SAFE, note or option holder with no shares yet). Its
-- holder details are a holder_record document, and a share class with its rights a share_class document. Each is
-- drafted, takes its evidence (an identity document; the constitution and the shareholders' agreement), and is
-- recorded on another person's approval (FM-1): append-only from then on, a later one superseding it. The database is
-- the backstop:
--   - an individual holder's identifier is kept as its type and its last 4 characters only (the owner's decision of 7
--     Nov 2026), a holder record holds no key outside its own, and none of its text shows an NRIC or FIN in full
--     (FY285);
--   - a share class's account is an active share-capital account (FY286);
--   - a share issue names a recorded class of its own account, and must name one once its account has a recorded
--     class (FY287).
-- The widened and new checks are added NOT VALID and validated by 000251 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.contact ADD COLUMN is_investor boolean NOT NULL DEFAULT false;
ALTER TABLE fyniq.contact DROP CONSTRAINT contact_role_known;
ALTER TABLE fyniq.contact ADD CONSTRAINT contact_role_known
  CHECK (is_customer OR is_vendor OR is_director OR is_shareholder OR is_investor) NOT VALID;
ALTER TABLE fyniq.contact DROP CONSTRAINT contact_member_person;
ALTER TABLE fyniq.contact ADD CONSTRAINT contact_member_person
  CHECK (member_user_id IS NULL OR is_director OR is_shareholder OR is_investor) NOT VALID;
GRANT INSERT (is_investor), UPDATE (is_investor) ON fyniq.contact TO fyniq_api;

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation', 'bill', 'bill_payment', 'vendor_credit', 'credit_application',
                  'supplier_refund', 'purchase_order', 'invoice', 'customer_receipt', 'write_off', 'credit_note',
                  'customer_credit_application', 'customer_refund', 'customer_deposit', 'quote', 'sales_order', 'statement',
                  'bank_transaction', 'bank_agreement', 'expense_claim', 'claim_reimbursement', 'cash_advance', 'fixed_asset',
                  'asset_disposal', 'payee_bank_change', 'claimant_bank_change', 'payment_batch',
                  'pre_incorporation_cost', 'director_pay_choice', 'director_fee', 'exempt_status',
                  'director_confirmation', 'director_settlement', 'director_repayment', 'account_merge',
                  'owner_succession', 'holder_record', 'share_class')) NOT VALID;

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
                               ('holder_record', 'holder_record.record'), ('share_class', 'share_class.record'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset',
                  'payee_bank_change', 'claimant_bank_change', 'payment_batch', 'contact_member_link',
                  'director_fee', 'director_settlement', 'director_repayment', 'account', 'bank_reconciliation',
                  'holder_record', 'share_class')) NOT VALID;

-- +goose StatementBegin
-- A holder record's or share class's version keeps its rules whoever writes it (FY285, FY286).
CREATE FUNCTION fyniq.equity_record_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t     text;
  p     jsonb := NEW.payload;
  extra text;
  ok    boolean;
BEGIN
  SELECT d.type INTO t FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id;
  IF t = 'holder_record' THEN
    SELECT k INTO extra FROM jsonb_object_keys(p) k
     WHERE k NOT IN ('date', 'contact_id', 'kind', 'id_type', 'id_last4', 'registration_number', 'nationality', 'address',
                     'contact_people', 'note')
     LIMIT 1;
    IF extra IS NOT NULL THEN
      RAISE EXCEPTION 'A holder record keeps no %', extra USING ERRCODE = 'FY285',
        HINT = 'It holds the holder''s kind, identifier, nationality, address and contact people only.';
    END IF;
    IF p->>'kind' = 'individual' THEN
      IF p ? 'registration_number' OR NOT coalesce(p->>'id_type', 'nric') IN ('nric', 'fin', 'passport')
         OR NOT coalesce(p->>'id_last4', 'X') ~ '^[A-Za-z0-9]{1,4}$' THEN
        RAISE EXCEPTION 'An individual''s identifier is kept as its type and its last 4 characters only' USING ERRCODE = 'FY285',
          HINT = 'The full number stays in ACRA''s register and the identity document kept as evidence (FR-EQ-15).';
      END IF;
    ELSIF p->>'kind' = 'entity' THEN
      IF p ? 'id_last4' OR NOT coalesce(p->>'id_type', 'uen') IN ('uen', 'foreign_registration')
         OR length(coalesce(p->>'registration_number', '')) > 30 THEN
        RAISE EXCEPTION 'An entity is identified by its UEN or foreign registration number' USING ERRCODE = 'FY285';
      END IF;
    ELSE
      RAISE EXCEPTION 'A holder is an individual or an entity' USING ERRCODE = 'FY285';
    END IF;
    IF p::text ~ '[STFGMstfgm][0-9]{7}[A-Za-z]' THEN
      RAISE EXCEPTION 'A holder record never shows an NRIC or FIN in full' USING ERRCODE = 'FY285',
        HINT = 'Keep the last 4 characters only (FR-EQ-15, CMP-PD-07).';
    END IF;
  ELSIF t = 'share_class' THEN
    SELECT EXISTS (SELECT 1 FROM fyniq.account a
                    WHERE a.organisation_id = NEW.organisation_id AND a.id::text = p->>'account_id'
                      AND a.role = 'share-capital' AND a.is_active AND NOT a.is_group)
      INTO ok;
    IF NOT ok THEN
      RAISE EXCEPTION 'A share class belongs to an active share-capital account' USING ERRCODE = 'FY286',
        HINT = 'Choose 3010 for ordinary shares or 3020 for preference shares (FR-EQ-16).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER equity_record_guard BEFORE INSERT ON fyniq.document_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.equity_record_guard();

ALTER TABLE fyniq.share_issue ADD COLUMN class_id uuid;
ALTER TABLE fyniq.share_issue ADD CONSTRAINT share_issue_class_document_fkey FOREIGN KEY (organisation_id, class_id)
  REFERENCES fyniq.document (organisation_id, id) NOT VALID;
GRANT INSERT (class_id) ON fyniq.share_issue TO fyniq_api;

-- +goose StatementBegin
-- A share issue names a recorded class of its own account, and must once its account has one (FY287). The class is
-- named by its first version's document; a later version of it supersedes the rights, never the account.
CREATE FUNCTION fyniq.share_issue_class_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  account text;
  status  text;
BEGIN
  IF NEW.class_id IS NULL THEN
    IF EXISTS (SELECT 1 FROM fyniq.document d JOIN fyniq.document_version v
                 ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
                WHERE d.organisation_id = NEW.organisation_id AND d.type = 'share_class' AND d.status = 'posted'
                  AND v.payload->>'account_id' = NEW.class_account_id::text) THEN
      RAISE EXCEPTION 'The share issue names no class, and its account has a recorded class' USING ERRCODE = 'FY287',
        HINT = 'Name the class the shares are allotted in (FR-EQ-16).';
    END IF;
    RETURN NEW;
  END IF;
  SELECT d.status, v.payload->>'account_id' INTO status, account
    FROM fyniq.document d JOIN fyniq.document_version v
      ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
   WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.class_id AND d.type = 'share_class';
  IF status IS DISTINCT FROM 'posted' OR account IS DISTINCT FROM NEW.class_account_id::text THEN
    RAISE EXCEPTION 'The share issue''s class is not a recorded class of its account' USING ERRCODE = 'FY287';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER share_issue_class_guard BEFORE INSERT ON fyniq.share_issue
  FOR EACH ROW EXECUTE FUNCTION fyniq.share_issue_class_guard();
