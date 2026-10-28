-- Claimants' bank details (PRD-001 FR-AP-22, FM-1, SEC-11, AUD-02; the owner's decisions of 27 Oct 2026, rev 0.154;
-- p1-payments-out task 1.1, design D9).
--
-- A person's own bank account for reimbursements changes as a vendor's does (000180), through a document of its own
-- type, claimant_bank_change: prepared by the person as a draft with the file that supports it, asked for, and approved
-- by someone who holds A on "Payment batches", never the person, whose approval posts it with no entry. Its account is
-- kept in payee_bank_account, which now names either a vendor (contact_id) or a person (member_user_id), exactly one.
-- The database is the backstop, as for vendors:
--   - a person's change is posted only under an approved request for it that neither its requester, nor the person who
--     saved its current version, nor the person whose account it is signed, with its account in force at commit (FY237);
--   - a person's account belongs to a draft claimant change's current version, for the person, currency and kind that
--     version names: a Singapore account of an active member; it comes into force with its change, and is retired only
--     by another approved change of the person's account in the currency (FY238);
--   - one account in force per person and currency (payee_bank_account_member_in_force, built concurrently by 000183).
-- The widened checks, the exactly-one check and the member's key are added NOT VALID and validated by 000184.

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
                  'asset_disposal', 'payee_bank_change', 'claimant_bank_change')) NOT VALID;

ALTER TABLE fyniq.payee_bank_account ADD COLUMN member_user_id uuid;
-- A person's account has no vendor. The table's only client is the storage adapter, which reads the column as optional
-- from this change; the exactly-one check below keeps every row naming one payee.
-- squawk-ignore ban-drop-not-null
ALTER TABLE fyniq.payee_bank_account ALTER COLUMN contact_id DROP NOT NULL;
ALTER TABLE fyniq.payee_bank_account ADD CONSTRAINT payee_bank_account_one_payee
  CHECK ((contact_id IS NULL) <> (member_user_id IS NULL)) NOT VALID;
ALTER TABLE fyniq.payee_bank_account ADD CONSTRAINT payee_bank_account_member_fkey
  FOREIGN KEY (organisation_id, member_user_id) REFERENCES fyniq.user_membership (organisation_id, user_id) NOT VALID;

-- +goose StatementBegin
-- 000180's guard, for a vendor's account or a person's. An account belongs to a draft change's current version: a
-- payee_bank_change for a vendor, a claimant_bank_change for a person, with the payee, currency, last four characters
-- and fingerprint its version names, and for a person a Singapore account of an active member. It is kept until its
-- change is posted at that version, which puts it in force; it is retired only while another change for the same payee
-- and currency is posted with its account still kept. It changes in no other way, and is never deleted or truncated
-- (FY238).
CREATE OR REPLACE FUNCTION fyniq.payee_bank_account_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  d fyniq.document;
  p jsonb;
  kind text;
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'Payees'' bank accounts are never deleted' USING ERRCODE = 'FY238';
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'The bank account of change % is never deleted', OLD.change_id USING ERRCODE = 'FY238',
      HINT = 'A later approved change takes it out of force; its history stays (FR-CT-03, FR-AP-22).';
  END IF;
  kind := CASE WHEN NEW.member_user_id IS NULL THEN 'payee_bank_change' ELSE 'claimant_bank_change' END;
  IF TG_OP = 'UPDATE' THEN
    IF NOT ((OLD.state = 'kept' AND NEW.state = 'in_force') OR (OLD.state = 'in_force' AND NEW.state = 'retired'))
       OR (NEW.organisation_id, NEW.id, NEW.change_id, NEW.change_version, NEW.contact_id, NEW.member_user_id, NEW.currency,
           NEW.number_sealed, NEW.number_last4, NEW.number_fingerprint)
          IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.change_id, OLD.change_version, OLD.contact_id, OLD.member_user_id,
                            OLD.currency, OLD.number_sealed, OLD.number_last4, OLD.number_fingerprint) THEN
      RAISE EXCEPTION 'The bank account of change % changes only to come into force, and then to be retired', OLD.change_id
        USING ERRCODE = 'FY238', HINT = 'Prepare another change of the bank details and have it approved (FR-CT-03, FR-AP-22).';
    END IF;
    IF NEW.state = 'in_force' AND NOT EXISTS (
      SELECT 1 FROM fyniq.document x WHERE x.organisation_id = NEW.organisation_id AND x.id = NEW.change_id
        AND x.type = kind AND x.status = 'posted' AND x.current_version = NEW.change_version
    ) THEN
      RAISE EXCEPTION 'The bank account of change % comes into force only when the change is approved', NEW.change_id
        USING ERRCODE = 'FY238', HINT = 'Ask for approval of the change: a second person approves it (FR-CT-03, FR-AP-22, FM-1).';
    END IF;
    IF NEW.state = 'retired' AND NOT EXISTS (
      SELECT 1 FROM fyniq.payee_bank_account a
      JOIN fyniq.document l ON l.organisation_id = a.organisation_id AND l.id = a.change_id AND l.current_version = a.change_version
      WHERE a.organisation_id = NEW.organisation_id AND a.contact_id IS NOT DISTINCT FROM NEW.contact_id
        AND a.member_user_id IS NOT DISTINCT FROM NEW.member_user_id AND a.currency = NEW.currency
        AND a.change_id <> NEW.change_id AND a.state = 'kept' AND l.type = kind AND l.status = 'posted'
    ) THEN
      RAISE EXCEPTION 'The bank account of change % is retired only when another change for its payee and currency is approved',
        NEW.change_id USING ERRCODE = 'FY238', HINT = 'Prepare another change of the bank details and have it approved (FR-CT-03, FR-AP-22).';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.state <> 'kept' THEN
    RAISE EXCEPTION 'The bank account of change % is kept until the change is approved', NEW.change_id USING ERRCODE = 'FY238';
  END IF;
  SELECT * INTO d FROM fyniq.document WHERE organisation_id = NEW.organisation_id AND id = NEW.change_id;
  IF d.id IS NULL OR d.type <> kind OR d.status <> 'draft' OR d.current_version <> NEW.change_version THEN
    RAISE EXCEPTION 'A bank account belongs to the current version of a draft change of bank details, not to version % of %',
      NEW.change_version, NEW.change_id USING ERRCODE = 'FY238';
  END IF;
  SELECT v.payload INTO p FROM fyniq.document_version v
   WHERE v.organisation_id = NEW.organisation_id AND v.document_id = NEW.change_id AND v.version = NEW.change_version;
  IF p->>'contact_id' IS DISTINCT FROM NEW.contact_id::text OR p->>'member_user_id' IS DISTINCT FROM NEW.member_user_id::text
     OR p->>'currency' IS DISTINCT FROM NEW.currency OR p->>'number_last4' IS DISTINCT FROM NEW.number_last4
     OR p->>'number_fingerprint' IS DISTINCT FROM encode(NEW.number_fingerprint, 'hex') THEN
    RAISE EXCEPTION 'The bank account of change % is not the one its version names', NEW.change_id USING ERRCODE = 'FY238';
  END IF;
  IF NEW.contact_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM fyniq.contact c WHERE c.organisation_id = NEW.organisation_id AND c.id = NEW.contact_id AND c.is_vendor
  ) THEN
    RAISE EXCEPTION 'Contact % is not a vendor: only a vendor has bank details to pay', NEW.contact_id USING ERRCODE = 'FY238';
  END IF;
  IF NEW.member_user_id IS NOT NULL AND (p->>'kind' IS DISTINCT FROM 'singapore' OR NOT EXISTS (
    SELECT 1 FROM fyniq.user_membership m
     WHERE m.organisation_id = NEW.organisation_id AND m.user_id = NEW.member_user_id AND m.status = 'active'
  )) THEN
    RAISE EXCEPTION 'A person''s bank account for reimbursements is a Singapore account of an active member, not %''s',
      NEW.member_user_id USING ERRCODE = 'FY238', HINT = 'Enter a Singapore bank''s account (FR-AP-22).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT INSERT (member_user_id) ON fyniq.payee_bank_account TO fyniq_api;

-- +goose StatementBegin
-- 000180's FY237, for a vendor's change and a person's: a change of bank details is approved, by posting it, only under
-- an approved request for it, of its own type, that neither its requester, nor the person who saved its current version,
-- nor (for a person's change) the person whose account it is signed, with its account in force at commit (FR-CT-03,
-- FR-AP-22, SOD-1, FM-1: in every team mode).
CREATE OR REPLACE FUNCTION fyniq.payee_bank_change_approved() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  preparer text;
  person uuid;
BEGIN
  IF TG_OP = 'UPDATE' AND OLD.status = 'posted' THEN
    RETURN NULL;
  END IF;
  SELECT v.created_by INTO preparer FROM fyniq.document_version v
   WHERE v.organisation_id = NEW.organisation_id AND v.document_id = NEW.id AND v.version = NEW.current_version;
  SELECT a.member_user_id INTO person FROM fyniq.payee_bank_account a
   WHERE a.organisation_id = NEW.organisation_id AND a.change_id = NEW.id AND a.change_version = NEW.current_version;
  IF NOT EXISTS (
       SELECT 1 FROM fyniq.approval_request r
        WHERE r.organisation_id = NEW.organisation_id AND r.object_type = NEW.type AND r.operation = NEW.type || '.approve'
          AND r.object_key = NEW.id::text AND r.status = 'approved'
          AND NOT EXISTS (SELECT 1 FROM fyniq.approval_signature s
                           WHERE s.organisation_id = r.organisation_id AND s.request_id = r.id
                             AND (s.signer = r.requested_by OR 'user:' || s.signer::text = preparer OR s.signer = person)))
     OR NOT EXISTS (SELECT 1 FROM fyniq.payee_bank_account a
                     WHERE a.organisation_id = NEW.organisation_id AND a.change_id = NEW.id AND a.change_version = NEW.current_version
                       AND a.state = 'in_force') THEN
    RAISE EXCEPTION 'Change % of bank details is approved only by a second person, never the payee, with its account coming into force',
      NEW.id USING ERRCODE = 'FY237',
      HINT = 'Ask for approval: someone who did not prepare or ask for it, and whose account it is not, approves it (FR-CT-03, FR-AP-22, SOD-1, FM-1).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER claimant_bank_change_approved AFTER INSERT OR UPDATE OF status ON fyniq.document
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.type = 'claimant_bank_change' AND NEW.status = 'posted')
  EXECUTE FUNCTION fyniq.payee_bank_change_approved();

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
                               ('expense_claim', 'expense_claim.post'), ('claim_reimbursement', 'claim_reimbursement.void'),
                               ('cash_advance', 'cash_advance.void'), ('asset_disposal', 'asset_disposal.void'),
                               ('fixed_asset', 'fixed_asset.void'),
                               ('payee_bank_change', 'payee_bank_change.approve'),
                               ('claimant_bank_change', 'claimant_bank_change.approve'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset',
                  'payee_bank_change', 'claimant_bank_change')) NOT VALID;
