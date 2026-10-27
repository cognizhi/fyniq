-- Fixed assets (PRD-001 FR-FA-01, FR-FA-02, FR-FA-03, posting rules "Depreciation" and "Asset disposal"; rev 0.149;
-- p1-fixed-assets task 1.1, design D1, D3, D4 and D7).
--
-- A registered asset is a fixed_asset document (FA-), its content versioned; registering it posts nothing, because its
-- source already put its cost on the asset account. Its source is one posted bill line or bank-transaction part that
-- debits an asset account, recorded in fixed_asset_source: one asset per source line (fixed_asset_source_line_once),
-- and only a line on that account, as the source's current version saved it, whose entry, not reversed, debits that
-- account, an active account in the report group "Property, plant & equipment" or "Intangible assets" (FY235). An asset bought before the
-- books start has no source. So:
--   - a change of an asset (its next version) posts nothing of its own: document_edit_posted lets a fixed_asset's
--     version move without an entry;
--   - each month's depreciation is an entry of its own, asset_depreciation@1, whose source is the asset at a version:
--     journal_entry_asset_depreciation refuses one (not its reversal) that does not name a posted asset at its current
--     version (FY236), as FY172 does for releases;
--   - an asset_disposal document (DSP-) posts asset_disposal@1, and a bank line matches a sale's money as money recorded
--     by hand (bank_match_posting, FY228); voiding one is the approval object asset_disposal.void, approved in Founder
--     mode by a second person who holds A on "Fixed assets" (FM-1).
-- The widened checks are added NOT VALID and validated by 000173 (squawk: constraint-missing-not-valid).

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
                  'asset_disposal')) NOT VALID;
ALTER TABLE fyniq.document_sequence DROP CONSTRAINT document_sequence_type_known;
ALTER TABLE fyniq.document_sequence ADD CONSTRAINT document_sequence_type_known
  CHECK (document_type IN ('journal', 'bill', 'bill_payment', 'vendor_credit', 'supplier_refund', 'purchase_order', 'invoice',
                           'customer_receipt', 'write_off', 'credit_note', 'customer_refund', 'customer_deposit', 'quote',
                           'sales_order', 'statement', 'expense_claim', 'claim_reimbursement', 'cash_advance', 'fixed_asset',
                           'asset_disposal')) NOT VALID;

CREATE TABLE fyniq.fixed_asset_source (
  organisation_id uuid    NOT NULL DEFAULT fyniq.current_org(),
  id              uuid    NOT NULL DEFAULT uuidv7(),
  asset_id        uuid    NOT NULL,
  source_id       uuid    NOT NULL,
  source_version  integer NOT NULL,
  line            integer NOT NULL,
  account_id      uuid    NOT NULL,
  CONSTRAINT fixed_asset_source_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT fixed_asset_source_asset_once UNIQUE (organisation_id, asset_id),
  CONSTRAINT fixed_asset_source_asset_fkey FOREIGN KEY (organisation_id, asset_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT fixed_asset_source_version_fkey FOREIGN KEY (organisation_id, source_id, source_version)
    REFERENCES fyniq.document_version (organisation_id, document_id, version),
  CONSTRAINT fixed_asset_source_account_fkey FOREIGN KEY (organisation_id, account_id) REFERENCES fyniq.account (organisation_id, id),
  CONSTRAINT fixed_asset_source_line_once UNIQUE (organisation_id, source_id, line),
  CONSTRAINT fixed_asset_source_line_positive CHECK (line BETWEEN 1 AND 10000)
);

-- +goose StatementBegin
-- An asset's source is a line of a posted bill or categorised bank line, at its current version, whose entry debits an
-- active asset account of the register's report groups; it is recorded once, with its asset, and never changes (FY235).
CREATE FUNCTION fyniq.fixed_asset_source_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  a fyniq.account;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The source of asset % never changes or goes', OLD.asset_id USING ERRCODE = 'FY235',
      HINT = 'An asset keeps the bill line or bank line it was registered from (FR-FA-01).';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.asset_id AND d.type = 'fixed_asset') THEN
    RAISE EXCEPTION 'Document % is not a fixed asset', NEW.asset_id USING ERRCODE = 'FY235';
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.document d
    JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
    WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.source_id AND d.type IN ('bill', 'bank_transaction')
      AND d.status = 'posted' AND d.current_version = NEW.source_version
      AND v.payload->'lines'->(NEW.line - 1)->>'account_id' = NEW.account_id::text
  ) THEN
    RAISE EXCEPTION 'An asset is registered from a line on its account of a posted bill or categorised bank line at its current version, not from line % of document % version %',
      NEW.line, NEW.source_id, NEW.source_version USING ERRCODE = 'FY235';
  END IF;
  SELECT * INTO a FROM fyniq.account WHERE organisation_id = NEW.organisation_id AND id = NEW.account_id;
  IF a.id IS NULL OR a.is_group OR NOT a.is_active OR coalesce(a.report_group, '') NOT IN ('Property, plant & equipment', 'Intangible assets') THEN
    RAISE EXCEPTION 'Account % is not an active asset account of the register', a.code USING ERRCODE = 'FY235',
      HINT = 'An asset sits on an account whose report group is Property, plant & equipment or Intangible assets (FR-FA-01).';
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.journal_entry e
    JOIN fyniq.journal_line l ON l.organisation_id = e.organisation_id AND l.entry_id = e.id
    WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.source_id::text AND e.source_version = NEW.source_version
      AND e.reverses_entry_id IS NULL AND l.account_id = NEW.account_id AND l.base_amount > 0
      AND NOT EXISTS (SELECT 1 FROM fyniq.journal_entry r WHERE r.organisation_id = e.organisation_id AND r.reverses_entry_id = e.id)
  ) THEN
    RAISE EXCEPTION 'The entry of document % version % debits nothing to account %', NEW.source_id, NEW.source_version, a.code
      USING ERRCODE = 'FY235', HINT = 'Register an asset from the line that put its cost on the asset account.';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER fixed_asset_source_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.fixed_asset_source
  FOR EACH ROW EXECUTE FUNCTION fyniq.fixed_asset_source_guard();

ALTER TABLE fyniq.fixed_asset_source ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.fixed_asset_source FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.fixed_asset_source
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Recorded with its asset, whose creation is audited in the same transaction (AUD-01).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.fixed_asset_source
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document', 'asset_id');

REVOKE ALL ON fyniq.fixed_asset_source FROM PUBLIC;
GRANT SELECT ON fyniq.fixed_asset_source TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (asset_id, source_id, source_version, line, account_id) ON fyniq.fixed_asset_source TO fyniq_api;

-- +goose StatementBegin
CREATE FUNCTION fyniq.journal_entry_asset_depreciation() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.document d
    WHERE d.organisation_id = NEW.organisation_id AND d.id::text = NEW.source_id AND d.type = 'fixed_asset'
      AND d.status = 'posted' AND d.current_version = NEW.source_version
  ) THEN
    RAISE EXCEPTION 'Depreciation % names no registered asset at its current version', NEW.posting_identity
      USING ERRCODE = 'FY236', HINT = 'A month''s depreciation is of a registered asset''s current version (FR-FA-02).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER journal_entry_asset_depreciation BEFORE INSERT ON fyniq.journal_entry
  FOR EACH ROW WHEN (NEW.source_type = 'asset_depreciation' AND NEW.reverses_entry_id IS NULL)
  EXECUTE FUNCTION fyniq.journal_entry_asset_depreciation();

-- document_edit_posted as in 000146, and: a fixed asset's next version posts nothing of its own (its depreciation posts
-- month by month, each its own entry), so its version moves without one.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.document_edit_posted() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.type = 'fixed_asset' THEN
    RETURN NULL;
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.journal_entry e
    WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.id::text
      AND e.source_type NOT IN ('bill_release', 'invoice_release', 'bank_out_release', 'bank_in_release')
      AND e.source_version = NEW.current_version AND e.reverses_entry_id IS NULL
  ) THEN
    RAISE EXCEPTION 'The edit of document % to version % posts that version', NEW.id, NEW.current_version
      USING ERRCODE = 'FY055', HINT = 'An edit of a posted document posts its new version in the same transaction (FR-GL-12).';
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.journal_entry e
    JOIN fyniq.journal_entry r ON r.organisation_id = e.organisation_id AND r.reverses_entry_id = e.id
    WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.id::text
      AND e.source_type NOT IN ('bill_release', 'invoice_release', 'bank_out_release', 'bank_in_release')
      AND e.source_version = OLD.current_version AND e.reverses_entry_id IS NULL
  ) THEN
    RAISE EXCEPTION 'The edit of document % reverses the entry of version %', NEW.id, OLD.current_version
      USING ERRCODE = 'FY055', HINT = 'An edit of a posted document reverses the entry it replaces in the same transaction (FR-GL-12).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.bank_match_posting() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  l     fyniq.bank_line;
  kinds text[];
BEGIN
  SELECT * INTO l FROM fyniq.bank_line WHERE organisation_id = NEW.organisation_id AND id = NEW.line_id;
  kinds := CASE NEW.kind
    WHEN 'categorised' THEN ARRAY['bank_transaction']
    WHEN 'suspense' THEN ARRAY['bank_transaction']
    WHEN 'receipt' THEN ARRAY['customer_receipt']
    WHEN 'bill_payment' THEN ARRAY['bill_payment']
    ELSE ARRAY['customer_receipt', 'bill_payment', 'customer_deposit', 'customer_refund', 'supplier_refund', 'claim_reimbursement', 'cash_advance',
               'asset_disposal'] END;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.document d
    WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id AND d.type = ANY (kinds) AND d.status = 'posted'
      AND (d.type <> 'bank_transaction' OR EXISTS (
        SELECT 1 FROM fyniq.document_version v WHERE v.organisation_id = d.organisation_id AND v.document_id = d.id
          AND v.version = d.current_version AND v.payload->>'bank_line_id' = NEW.line_id::text))
      AND EXISTS (
        SELECT 1 FROM fyniq.journal_entry e
        JOIN fyniq.money_account m ON m.organisation_id = e.organisation_id AND m.id = l.money_account_id
        JOIN fyniq.journal_line jl ON jl.organisation_id = e.organisation_id AND jl.entry_id = e.id AND jl.account_id = m.account_id
        WHERE e.organisation_id = NEW.organisation_id AND e.source_id = d.id::text AND e.source_version = d.current_version
          AND e.reverses_entry_id IS NULL AND jl.currency = l.currency AND jl.amount = l.amount
          AND (NEW.entry_id IS NULL OR (e.id = NEW.entry_id AND e.posting_date = l.booking_date))
          AND NOT EXISTS (SELECT 1 FROM fyniq.journal_entry r WHERE r.organisation_id = e.organisation_id AND r.reverses_entry_id = e.id))
  ) THEN
    RAISE EXCEPTION 'The % match of bank line % names no document of that kind, or one that does not post the line''s amount through its account', NEW.kind, NEW.line_id
      USING ERRCODE = 'FY228', HINT = 'Match the line through Banking, which records its document with the line''s amount and date (FR-BNK-07).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

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
                               ('cash_advance', 'cash_advance.void'), ('asset_disposal', 'asset_disposal.void'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal')) NOT VALID;
