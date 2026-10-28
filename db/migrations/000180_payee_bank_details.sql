-- Vendor bank details (PRD-001 FR-CT-03, FR-CT-04, SOD-1, FM-1, SEC-11, AUD-02; the owner's decisions of 27 Oct 2026,
-- rev 0.152; p1-organisation-chart task 4.2).
--
-- A change to a vendor's bank account in one currency is a document of its own type, payee_bank_change: prepared as a
-- draft with the files that support it, asked for, and approved by a second person who holds A on "Vendor bank
-- details", whose approval posts it, with no entry. The account number is never in the document: payee_bank_account
-- keeps it for each version of a change, sealed by the application (SEC-11) and bound to its change, with its last four
-- characters and a keyed fingerprint (FR-CT-04, AUD-02), which the version's payload repeats. The database is the
-- backstop:
--   - a change is posted only under an approved request for it that neither its requester nor the person who saved its
--     current version signed, with its account coming into force in the same transaction (FY237);
--   - an account belongs to a draft change's current version, for the vendor and currency that version names; it is
--     kept until its change is posted, which puts it in force, and is retired only when another change for the vendor
--     and currency is posted, in the transaction that puts that one's account in force; it changes in no other way and
--     is never deleted (FY238);
--   - one account in force per vendor and currency (payee_bank_account_in_force).
-- The widened checks are added NOT VALID and validated by 000181 (squawk: constraint-missing-not-valid).

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
                  'asset_disposal', 'payee_bank_change')) NOT VALID;

CREATE TABLE fyniq.payee_bank_account (
  organisation_id    uuid    NOT NULL DEFAULT fyniq.current_org(),
  id                 uuid    NOT NULL DEFAULT uuidv7(),
  change_id          uuid    NOT NULL,
  change_version     integer NOT NULL,
  contact_id         uuid    NOT NULL,
  currency           text    NOT NULL,
  number_sealed      bytea   NOT NULL,
  number_last4       text    NOT NULL,
  number_fingerprint bytea   NOT NULL,
  state              text    NOT NULL DEFAULT 'kept',
  CONSTRAINT payee_bank_account_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT payee_bank_account_version_once UNIQUE (organisation_id, change_id, change_version),
  CONSTRAINT payee_bank_account_version_fkey FOREIGN KEY (organisation_id, change_id, change_version)
    REFERENCES fyniq.document_version (organisation_id, document_id, version),
  CONSTRAINT payee_bank_account_contact_fkey FOREIGN KEY (organisation_id, contact_id) REFERENCES fyniq.contact (organisation_id, id),
  CONSTRAINT payee_bank_account_currency_format CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT payee_bank_account_sealed_length CHECK (octet_length(number_sealed) BETWEEN 40 AND 2048),
  CONSTRAINT payee_bank_account_last4_format CHECK (number_last4 ~ '^[0-9A-Z]{1,4}$'),
  CONSTRAINT payee_bank_account_fingerprint_length CHECK (octet_length(number_fingerprint) = 32),
  CONSTRAINT payee_bank_account_state_known CHECK (state IN ('kept', 'in_force', 'retired'))
);
-- One account in force per vendor and currency (FR-CT-03).
CREATE UNIQUE INDEX payee_bank_account_in_force ON fyniq.payee_bank_account (organisation_id, contact_id, currency) WHERE state = 'in_force';
-- The accounts in force by fingerprint, for the same account at two vendors (FR-CT-04).
CREATE INDEX payee_bank_account_fingerprint ON fyniq.payee_bank_account (organisation_id, number_fingerprint) WHERE state = 'in_force';

-- +goose StatementBegin
-- An account belongs to a draft change's current version, for the vendor and currency it names, with the last four
-- characters and fingerprint it repeats. It is kept until its change is posted at that version, which puts it in force;
-- it is retired only while another change for the vendor and currency is posted with its account still kept, which
-- happens only in the transaction that approves that change (FY237 wants its account in force at commit). It changes in
-- no other way, and is never deleted or truncated (FY238).
CREATE FUNCTION fyniq.payee_bank_account_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  d fyniq.document;
  p jsonb;
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'Vendors'' bank accounts are never deleted' USING ERRCODE = 'FY238';
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'The bank account of change % is never deleted', OLD.change_id USING ERRCODE = 'FY238',
      HINT = 'A later approved change takes it out of force; its history stays (FR-CT-03).';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF NOT ((OLD.state = 'kept' AND NEW.state = 'in_force') OR (OLD.state = 'in_force' AND NEW.state = 'retired'))
       OR (NEW.organisation_id, NEW.id, NEW.change_id, NEW.change_version, NEW.contact_id, NEW.currency, NEW.number_sealed,
           NEW.number_last4, NEW.number_fingerprint)
          IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.change_id, OLD.change_version, OLD.contact_id, OLD.currency,
                            OLD.number_sealed, OLD.number_last4, OLD.number_fingerprint) THEN
      RAISE EXCEPTION 'The bank account of change % changes only to come into force, and then to be retired', OLD.change_id
        USING ERRCODE = 'FY238', HINT = 'Prepare another change of the bank details and have it approved (FR-CT-03).';
    END IF;
    IF NEW.state = 'in_force' AND NOT EXISTS (
      SELECT 1 FROM fyniq.document x WHERE x.organisation_id = NEW.organisation_id AND x.id = NEW.change_id
        AND x.type = 'payee_bank_change' AND x.status = 'posted' AND x.current_version = NEW.change_version
    ) THEN
      RAISE EXCEPTION 'The bank account of change % comes into force only when the change is approved', NEW.change_id
        USING ERRCODE = 'FY238', HINT = 'Ask for approval of the change: a second person approves it (FR-CT-03, FM-1).';
    END IF;
    IF NEW.state = 'retired' AND NOT EXISTS (
      SELECT 1 FROM fyniq.payee_bank_account a
      JOIN fyniq.document l ON l.organisation_id = a.organisation_id AND l.id = a.change_id AND l.current_version = a.change_version
      WHERE a.organisation_id = NEW.organisation_id AND a.contact_id = NEW.contact_id AND a.currency = NEW.currency
        AND a.change_id <> NEW.change_id AND a.state = 'kept' AND l.type = 'payee_bank_change' AND l.status = 'posted'
    ) THEN
      RAISE EXCEPTION 'The bank account of change % is retired only when another change for its vendor and currency is approved',
        NEW.change_id USING ERRCODE = 'FY238', HINT = 'Prepare another change of the bank details and have it approved (FR-CT-03).';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.state <> 'kept' THEN
    RAISE EXCEPTION 'The bank account of change % is kept until the change is approved', NEW.change_id USING ERRCODE = 'FY238';
  END IF;
  SELECT * INTO d FROM fyniq.document WHERE organisation_id = NEW.organisation_id AND id = NEW.change_id;
  IF d.id IS NULL OR d.type <> 'payee_bank_change' OR d.status <> 'draft' OR d.current_version <> NEW.change_version THEN
    RAISE EXCEPTION 'A bank account belongs to the current version of a draft change of bank details, not to version % of %',
      NEW.change_version, NEW.change_id USING ERRCODE = 'FY238';
  END IF;
  SELECT v.payload INTO p FROM fyniq.document_version v
   WHERE v.organisation_id = NEW.organisation_id AND v.document_id = NEW.change_id AND v.version = NEW.change_version;
  IF p->>'contact_id' IS DISTINCT FROM NEW.contact_id::text OR p->>'currency' IS DISTINCT FROM NEW.currency
     OR p->>'number_last4' IS DISTINCT FROM NEW.number_last4 OR p->>'number_fingerprint' IS DISTINCT FROM encode(NEW.number_fingerprint, 'hex') THEN
    RAISE EXCEPTION 'The bank account of change % is not the one its version names', NEW.change_id USING ERRCODE = 'FY238';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.contact c WHERE c.organisation_id = NEW.organisation_id AND c.id = NEW.contact_id AND c.is_vendor) THEN
    RAISE EXCEPTION 'Contact % is not a vendor: only a vendor has bank details to pay', NEW.contact_id USING ERRCODE = 'FY238';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER payee_bank_account_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.payee_bank_account
  FOR EACH ROW EXECUTE FUNCTION fyniq.payee_bank_account_guard();
CREATE TRIGGER payee_bank_account_no_truncate BEFORE TRUNCATE ON fyniq.payee_bank_account
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.payee_bank_account_guard();

ALTER TABLE fyniq.payee_bank_account ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.payee_bank_account FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.payee_bank_account
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Kept with its change's version, and put in force or retired with a change's approval: each audited on its change in
-- the same transaction (AUD-01), with the last four characters and the fingerprint only (AUD-02).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.payee_bank_account
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document', 'change_id');

REVOKE ALL ON fyniq.payee_bank_account FROM PUBLIC;
GRANT SELECT ON fyniq.payee_bank_account TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (change_id, change_version, contact_id, currency, number_sealed, number_last4, number_fingerprint) ON fyniq.payee_bank_account TO fyniq_api;
GRANT UPDATE (state) ON fyniq.payee_bank_account TO fyniq_api;

-- +goose StatementBegin
-- A change of bank details is approved, by posting it, only under an approved request for it that neither its requester
-- nor the person who saved its current version signed, and with its account in force at commit (FY237; FR-CT-03,
-- SOD-1, FM-1: in every team mode).
CREATE FUNCTION fyniq.payee_bank_change_approved() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  preparer text;
BEGIN
  IF TG_OP = 'UPDATE' AND OLD.status = 'posted' THEN
    RETURN NULL;
  END IF;
  SELECT v.created_by INTO preparer FROM fyniq.document_version v
   WHERE v.organisation_id = NEW.organisation_id AND v.document_id = NEW.id AND v.version = NEW.current_version;
  IF NOT EXISTS (
       SELECT 1 FROM fyniq.approval_request r
        WHERE r.organisation_id = NEW.organisation_id AND r.object_type = 'payee_bank_change' AND r.operation = 'payee_bank_change.approve'
          AND r.object_key = NEW.id::text AND r.status = 'approved'
          AND NOT EXISTS (SELECT 1 FROM fyniq.approval_signature s
                           WHERE s.organisation_id = r.organisation_id AND s.request_id = r.id
                             AND (s.signer = r.requested_by OR 'user:' || s.signer::text = preparer)))
     OR NOT EXISTS (SELECT 1 FROM fyniq.payee_bank_account a
                     WHERE a.organisation_id = NEW.organisation_id AND a.change_id = NEW.id AND a.change_version = NEW.current_version
                       AND a.state = 'in_force') THEN
    RAISE EXCEPTION 'Change % of bank details is approved only by a second person, with its account coming into force', NEW.id
      USING ERRCODE = 'FY237', HINT = 'Ask for approval: someone who did not prepare or ask for it approves it (FR-CT-03, SOD-1, FM-1).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER payee_bank_change_approved AFTER INSERT OR UPDATE OF status ON fyniq.document
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.type = 'payee_bank_change' AND NEW.status = 'posted')
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
                               ('payee_bank_change', 'payee_bank_change.approve'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset',
                  'payee_bank_change')) NOT VALID;
