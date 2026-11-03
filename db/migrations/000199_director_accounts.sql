-- Director accounts (PRD-001 FR-EQ-01, B.7, FM-1; the owner's decisions of 3 Nov 2026, rev 0.167; p1-founders-money
-- task 1.1, design D1 and D2).
--
-- A director account belongs to a contact marked director or shareholder, optionally linked to the member who signs in
-- as them. Every journal line on an account with the role of 2500 (director-loan) or 2120 (deferred-remuneration) names
-- that contact as its sub-ledger key, journal_line.party_id. The database is the backstop:
--   - journal_line_check refuses such a line without a party, a party that is not a director or shareholder of the
--     organisation, and a party on any other account (FY250);
--   - a contact named by a line stays a director or shareholder (FY249);
--   - a contact is created with no member, and its member changes only under an approved request for the change, at the
--     contact's version the request names, that neither its requester nor the member linked or unlinked signed
--     (FY251; FM-1 in every team mode): the member decides where repayments go and who never approves their own
--     settlement (FM-9).
-- The widened and new constraints are added NOT VALID and validated by 000201; the line's party index and one contact per
-- member are built concurrently by 000200.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.contact ADD COLUMN is_director boolean NOT NULL DEFAULT false;
ALTER TABLE fyniq.contact ADD COLUMN is_shareholder boolean NOT NULL DEFAULT false;
ALTER TABLE fyniq.contact ADD COLUMN member_user_id uuid;
ALTER TABLE fyniq.contact DROP CONSTRAINT contact_role_known;
ALTER TABLE fyniq.contact ADD CONSTRAINT contact_role_known
  CHECK (is_customer OR is_vendor OR is_director OR is_shareholder) NOT VALID;
ALTER TABLE fyniq.contact ADD CONSTRAINT contact_member_person
  CHECK (member_user_id IS NULL OR is_director OR is_shareholder) NOT VALID;
ALTER TABLE fyniq.contact ADD CONSTRAINT contact_member_fkey FOREIGN KEY (organisation_id, member_user_id)
  REFERENCES fyniq.user_membership (organisation_id, user_id) NOT VALID;
-- The roles are entered with the contact; the member only through its approved change, whose execution runs as the API.
GRANT INSERT (is_director, is_shareholder), UPDATE (is_director, is_shareholder, member_user_id) ON fyniq.contact TO fyniq_api;

ALTER TABLE fyniq.journal_line ADD COLUMN party_id uuid;
ALTER TABLE fyniq.journal_line ADD CONSTRAINT journal_line_party_fkey FOREIGN KEY (organisation_id, party_id)
  REFERENCES fyniq.contact (organisation_id, id) NOT VALID;
GRANT INSERT (party_id) ON fyniq.journal_line TO fyniq_api, fyniq_worker, fyniq_executor;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.journal_line_check() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
  e   record;
  a   record;
  o   record;
BEGIN
  SELECT posting_date, created_xact, source_type INTO e FROM fyniq.journal_entry
   WHERE organisation_id = org AND id = NEW.entry_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'Journal entry % does not exist in this organisation', NEW.entry_id
      USING ERRCODE = 'FY021';
  END IF;
  IF e.created_xact <> pg_current_xact_id()::text::bigint THEN
    RAISE EXCEPTION 'Journal entry % is posted, so no line can be added to it', NEW.entry_id
      USING ERRCODE = 'FY002',
            HINT = 'Correct posted history with a reversing entry (INV-02, FR-GL-12).';
  END IF;

  SELECT code, is_group, is_active, role INTO a FROM fyniq.account
   WHERE organisation_id = org AND id = NEW.account_id
   FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'Account % does not exist in this organisation', NEW.account_id
      USING ERRCODE = 'FY004';
  END IF;
  IF a.is_group THEN
    RAISE EXCEPTION 'Account % is a group account, so it cannot take postings', a.code
      USING ERRCODE = 'FY004', HINT = 'Post to an account under it (INV-04, FR-COA-05).';
  END IF;
  IF NOT a.is_active THEN
    RAISE EXCEPTION 'Account % is inactive, so it cannot take postings', a.code
      USING ERRCODE = 'FY004', HINT = 'Reactivate it or post to another account (INV-04).';
  END IF;
  -- (000054) INV-13: share capital changes only through the register of share issues or the opening balances, their
  -- reversals included; every other source is refused.
  IF a.role = 'share-capital' AND e.source_type NOT IN ('share_issue', 'opening_balance') THEN
    RAISE EXCEPTION 'Account % is share capital, so a % cannot post to it', a.code, replace(e.source_type, '_', ' ')
      USING ERRCODE = 'FY007',
            HINT = 'Record the share issue in the register of share issues, or the balance in the opening balances (INV-13).';
  END IF;
  -- (000199) B.7, FR-EQ-01: a line on a director's account names its person, a director or shareholder, held for share
  -- so that no one unmarks them meanwhile (FY249); no other line names one.
  IF a.role IN ('director-loan', 'deferred-remuneration') THEN
    IF NEW.party_id IS NULL THEN
      RAISE EXCEPTION 'Account % is a director''s account, so each line on it names whose it is', a.code
        USING ERRCODE = 'FY250', HINT = 'Choose the director or shareholder the amount belongs to (FR-EQ-01, B.7).';
    END IF;
    PERFORM 1 FROM fyniq.contact c
      WHERE c.organisation_id = org AND c.id = NEW.party_id AND (c.is_director OR c.is_shareholder)
      FOR SHARE;
    IF NOT FOUND THEN
      RAISE EXCEPTION 'Contact % is not a director or shareholder of this organisation, so it has no director''s account',
        NEW.party_id
        USING ERRCODE = 'FY250', HINT = 'Mark the contact director or shareholder, or choose another (FR-EQ-01).';
    END IF;
  ELSIF NEW.party_id IS NOT NULL THEN
    RAISE EXCEPTION 'Account % is not a director''s account, so its line names no person', a.code
      USING ERRCODE = 'FY250', HINT = 'Leave the person out, or post to the director''s account (B.7).';
  END IF;

  SELECT base_currency, base_minor_units INTO o FROM fyniq.organisation WHERE organisation_id = org;
  NEW.organisation_id := org;
  NEW.posting_date := e.posting_date;
  NEW.minor_units := (SELECT c.minor_units FROM fyniq.currency c WHERE c.code = NEW.currency);
  IF NEW.minor_units IS NULL THEN
    RAISE EXCEPTION 'Currency % is not an ISO 4217 code Fyniq knows', NEW.currency
      USING ERRCODE = 'FY005';
  END IF;
  NEW.base_currency := o.base_currency;
  NEW.base_minor_units := o.base_minor_units;
  NEW.recorded_at := now();
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- FY249 and FY251's first half: a contact named by a journal line stays a director or shareholder, and a contact is
-- created with no member.
CREATE FUNCTION fyniq.contact_party_guard() RETURNS trigger
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
     AND EXISTS (SELECT 1 FROM fyniq.journal_line l WHERE l.organisation_id = OLD.organisation_id AND l.party_id = OLD.id) THEN
    RAISE EXCEPTION 'Contact % has amounts on a director''s account, so it stays a director or shareholder', OLD.legal_name
      USING ERRCODE = 'FY249', HINT = 'Keep the role; the account''s history names the person (FR-EQ-01, B.7).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER contact_party_guard BEFORE INSERT OR UPDATE ON fyniq.contact
  FOR EACH ROW EXECUTE FUNCTION fyniq.contact_party_guard();

-- +goose StatementBegin
-- FY251's second half: a contact's member changes only under an approved request for that change, at the contact's
-- version before it, that neither its requester nor the member linked or unlinked signed (FR-EQ-01, FM-1, FM-9).
CREATE FUNCTION fyniq.contact_member_approved() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (
       SELECT 1 FROM fyniq.approval_request r
        WHERE r.organisation_id = NEW.organisation_id AND r.object_type = 'contact_member_link'
          AND r.operation = 'contact_member_link.change' AND r.object_key = NEW.id::text AND r.status = 'approved'
          AND r.change #>> '{execute,member_user_id}' IS NOT DISTINCT FROM NEW.member_user_id::text
          AND r.change #>> '{execute,version}' = OLD.version::text
          AND NOT EXISTS (SELECT 1 FROM fyniq.approval_signature s
                           WHERE s.organisation_id = r.organisation_id AND s.request_id = r.id
                             AND (s.signer = r.requested_by OR s.signer = NEW.member_user_id
                                  OR s.signer = OLD.member_user_id))) THEN
    RAISE EXCEPTION 'The member of contact % changes only by a second person''s approval, never the member''s',
      NEW.legal_name USING ERRCODE = 'FY251',
      HINT = 'Ask for approval: someone who did not ask for it, and is not the member linked or unlinked, approves it (FR-EQ-01, FM-1).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER contact_member_approved AFTER UPDATE OF member_user_id ON fyniq.contact
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.member_user_id IS DISTINCT FROM OLD.member_user_id)
  EXECUTE FUNCTION fyniq.contact_member_approved();

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
                               ('contact_member_link', 'contact_member_link.change'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset',
                  'payee_bank_change', 'claimant_bank_change', 'payment_batch', 'contact_member_link')) NOT VALID;
