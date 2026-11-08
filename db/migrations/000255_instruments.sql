-- SAFEs and convertible notes (PRD-001 FR-EQ-18, G4-044; p1-equity-register slice 4a, task 5.1, design D6).
--
-- An instrument is an instrument document: a SAFE (post-money or pre-money with a valuation cap, discount only, or
-- most-favoured-nation) or a convertible note, with its holder, amount, the day it was signed, its cap, discount,
-- interest and maturity, what triggers its conversion, the class it converts into, how its terms count the shares, the
-- conversion price's precision and the accountant's classification. It is drafted, takes its evidence (the signed
-- instrument), and is recorded on another person's approval (FM-1), as a holder record is (000250).
--
-- An instrument template is a form the engine may convert by itself: an organisation enables one by recording an
-- instrument_template document with its lawyer's confirmation as evidence, on another person's approval (question 33).
-- Only yc-sg-post-money-cap@1 is known, YC's "SAFE: Valuation Cap, No Discount (Singapore)".
--
-- The database is the backstop (FY291):
--   - an instrument's version keeps its own keys, a known form, a holder who is a shareholder or investor, an amount
--     above zero at cents, and an active share-capital account to convert into;
--   - a template's version names a known template;
--   - a contact who holds a recorded instrument stays a shareholder or investor.
-- The widened checks are added NOT VALID and validated by 000256 (squawk: constraint-missing-not-valid).

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
                  'instrument_template')) NOT VALID;

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
                               ('instrument_template', 'instrument_template.enable'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset',
                  'payee_bank_change', 'claimant_bank_change', 'payment_batch', 'contact_member_link',
                  'director_fee', 'director_settlement', 'director_repayment', 'account', 'bank_reconciliation',
                  'holder_record', 'share_class', 'equity_transaction', 'instrument', 'instrument_template')) NOT VALID;

-- +goose StatementBegin
-- An instrument's or instrument template's version keeps its rules whoever writes it (FY291).
CREATE FUNCTION fyniq.instrument_guard() RETURNS trigger
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
  IF t = 'instrument' THEN
    SELECT k INTO extra FROM jsonb_object_keys(p) k
     WHERE k NOT IN ('date', 'form', 'holder_id', 'amount', 'currency', 'signed_on', 'valuation_cap', 'discount', 'mfn',
                     'interest_rate', 'maturity_on', 'trigger', 'class_account_id', 'class_id', 'denominator',
                     'denominator_note', 'interest_converts', 'price_decimals', 'classification', 'template', 'pro_rata',
                     'note')
     LIMIT 1;
    IF extra IS NOT NULL THEN
      RAISE EXCEPTION 'An instrument keeps no %', extra USING ERRCODE = 'FY291',
        HINT = 'It holds its form, holder, amount, terms and the class it converts into only (FR-EQ-18).';
    END IF;
    IF NOT coalesce(p->>'form', '') IN ('post_money_safe', 'pre_money_safe', 'discount_safe', 'mfn_safe', 'convertible_note') THEN
      RAISE EXCEPTION 'An instrument is a SAFE or a convertible note of a known form' USING ERRCODE = 'FY291',
        HINT = 'Choose post_money_safe, pre_money_safe, discount_safe, mfn_safe or convertible_note (FR-EQ-18).';
    END IF;
    SELECT EXISTS (SELECT 1 FROM fyniq.contact c
                    WHERE c.organisation_id = NEW.organisation_id AND c.id::text = p->>'holder_id'
                      AND (c.is_shareholder OR c.is_investor))
      INTO ok;
    IF NOT ok THEN
      RAISE EXCEPTION 'An instrument''s holder is a contact marked shareholder or investor' USING ERRCODE = 'FY291',
        HINT = 'Mark the contact an investor in Contacts first (FR-EQ-15, FR-EQ-18).';
    END IF;
    IF NOT coalesce(p->>'amount', '') ~ '^[0-9]{1,14}(\.[0-9]{1,2})?$' OR (p->>'amount')::numeric <= 0 THEN
      RAISE EXCEPTION 'An instrument''s amount is above zero, at most to the cent' USING ERRCODE = 'FY291';
    END IF;
    SELECT EXISTS (SELECT 1 FROM fyniq.account a
                    WHERE a.organisation_id = NEW.organisation_id AND a.id::text = p->>'class_account_id'
                      AND a.role = 'share-capital' AND a.is_active AND NOT a.is_group)
      INTO ok;
    IF NOT ok THEN
      RAISE EXCEPTION 'An instrument converts into the class of an active share-capital account' USING ERRCODE = 'FY291',
        HINT = 'Choose 3010 for ordinary shares or 3020 for preference shares (FR-EQ-18).';
    END IF;
    IF p ? 'template' AND NOT p->>'template' IN ('yc-sg-post-money-cap@1') THEN
      RAISE EXCEPTION 'Instrument template % is not one Fyniq knows', p->>'template' USING ERRCODE = 'FY291';
    END IF;
  ELSIF t = 'instrument_template' THEN
    SELECT k INTO extra FROM jsonb_object_keys(p) k WHERE k NOT IN ('date', 'template', 'note') LIMIT 1;
    IF extra IS NOT NULL OR NOT coalesce(p->>'template', '') IN ('yc-sg-post-money-cap@1') THEN
      RAISE EXCEPTION 'An instrument template names a template Fyniq knows, and nothing else' USING ERRCODE = 'FY291',
        HINT = 'Enable yc-sg-post-money-cap@1 with its lawyer''s confirmation (FR-EQ-18, question 33).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER instrument_guard BEFORE INSERT ON fyniq.document_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.instrument_guard();

-- +goose StatementBegin
-- (000252, 000255) FY249 and FY251's first half: a contact with lines on a director's account stays a director or
-- shareholder, one with lines on share application monies or a recorded instrument stays a shareholder or investor, and
-- a contact is created with no member.
CREATE OR REPLACE FUNCTION fyniq.contact_party_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.member_user_id IS NOT NULL THEN
      RAISE EXCEPTION 'Contact % is created with no member: its member is linked by an approved change', NEW.legal_name
        USING ERRCODE = 'FY251', HINT = 'Create the contact, then ask for its member to be linked (FR-EQ-01, FM-1).';
    END IF;
    RETURN NEW;
  END IF;
  IF (OLD.is_director OR OLD.is_shareholder) AND NOT (NEW.is_director OR NEW.is_shareholder)
     AND EXISTS (SELECT 1 FROM fyniq.journal_line l JOIN fyniq.account a ON a.organisation_id = l.organisation_id AND a.id = l.account_id
                  WHERE l.organisation_id = OLD.organisation_id AND l.party_id = OLD.id
                    AND a.role IN ('director-loan', 'deferred-remuneration')) THEN
    RAISE EXCEPTION 'Contact % has amounts on a director''s account, so it stays a director or shareholder', OLD.legal_name
      USING ERRCODE = 'FY249', HINT = 'Keep the role; the account''s history names the person (FR-EQ-01, B.7).';
  END IF;
  IF (OLD.is_shareholder OR OLD.is_investor) AND NOT (NEW.is_shareholder OR NEW.is_investor)
     AND EXISTS (SELECT 1 FROM fyniq.journal_line l JOIN fyniq.account a ON a.organisation_id = l.organisation_id AND a.id = l.account_id
                  WHERE l.organisation_id = OLD.organisation_id AND l.party_id = OLD.id AND a.role = 'share-application-monies') THEN
    RAISE EXCEPTION 'Contact % has share application monies, so it stays a shareholder or investor', OLD.legal_name
      USING ERRCODE = 'FY249', HINT = 'Keep the mark; the account''s history names the subscriber (B.7).';
  END IF;
  IF (OLD.is_shareholder OR OLD.is_investor) AND NOT (NEW.is_shareholder OR NEW.is_investor)
     AND EXISTS (SELECT 1 FROM fyniq.document d
                   JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id
                                                AND v.version = d.current_version
                  WHERE d.organisation_id = OLD.organisation_id AND d.type = 'instrument' AND d.status = 'posted'
                    AND v.payload->>'holder_id' = OLD.id::text) THEN
    RAISE EXCEPTION 'Contact % holds a recorded SAFE or convertible note, so it stays a shareholder or investor', OLD.legal_name
      USING ERRCODE = 'FY249', HINT = 'Keep the mark; the instrument names its holder (FR-EQ-18).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
