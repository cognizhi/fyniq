-- Voiding a fixed asset registered by mistake (PRD-001 FR-FA-01, FR-GL-12, FM-1; rev 0.151; p1-fixed-assets task 7.3,
-- design D9).
--
-- A void reverses the asset's months of depreciation on their own dates, sets its document to reversed, and releases
-- its source, so its line can be registered again while the voided asset stays in the history. fixed_asset_source
-- gains released, false until then. One asset per line becomes one live asset per line: 000178 builds the unique index
-- on the line where not released, concurrently, and 000179 drops fixed_asset_source_line_once. FY235 now lets a source
-- change in one way only, from live to released, in the transaction that reversed its asset (whose status change is
-- audited there, as require_audit checks on the update too), and never lets one go, for any role. In Founder mode a
-- void is the approval object fixed_asset with fixed_asset.void, approved by a second person who holds A on "Fixed
-- assets" (FM-1). The widened checks are added NOT VALID and validated by 000179 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.fixed_asset_source ADD COLUMN released boolean NOT NULL DEFAULT false;

-- +goose StatementBegin
-- An asset's source is a line of a posted bill or categorised bank line, at its current version, whose entry debits an
-- active asset account of the register's report groups; it is recorded once, with its asset, and changes only to be
-- released when its asset is voided; it never goes (FY235).
CREATE OR REPLACE FUNCTION fyniq.fixed_asset_source_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  a fyniq.account;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'The source of asset % never goes', OLD.asset_id USING ERRCODE = 'FY235',
      HINT = 'Void the asset instead: its source stays, released (FR-FA-01).';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF OLD.released OR NOT NEW.released
       OR (NEW.organisation_id, NEW.id, NEW.asset_id, NEW.source_id, NEW.source_version, NEW.line, NEW.account_id)
          IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.asset_id, OLD.source_id, OLD.source_version, OLD.line, OLD.account_id) THEN
      RAISE EXCEPTION 'The source of asset % changes only to be released', OLD.asset_id USING ERRCODE = 'FY235',
        HINT = 'An asset keeps the bill line or bank line it was registered from until it is voided (FR-FA-01).';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM fyniq.document d WHERE d.organisation_id = OLD.organisation_id AND d.id = OLD.asset_id
                   AND d.type = 'fixed_asset' AND d.status = 'reversed') THEN
      RAISE EXCEPTION 'The source of asset % is released only once the asset is voided', OLD.asset_id USING ERRCODE = 'FY235',
        HINT = 'Void the asset, which releases its line in the same transaction (FR-FA-01).';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.released THEN
    RAISE EXCEPTION 'The source of asset % is recorded live', NEW.asset_id USING ERRCODE = 'FY235';
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

-- document_reversed_posted as in 000146, and: a fixed asset posts nothing of its own, so its void needs no entry of its
-- version reversed, only every month of its depreciation, of any version, reversed (none, if none posted).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.document_reversed_posted() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.type IN ('purchase_order', 'quote', 'sales_order') THEN
    RETURN NULL; -- a purchase order, a quote or a sales order posts nothing, so closing or cancelling it reverses nothing
  END IF;
  IF NEW.type = 'fixed_asset' THEN -- registering posts nothing; a void reverses every month of its depreciation, if any posted
    IF EXISTS (
      SELECT 1 FROM fyniq.journal_entry e
      WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.id::text AND e.source_type = 'asset_depreciation'
        AND e.reverses_entry_id IS NULL
        AND NOT EXISTS (SELECT 1 FROM fyniq.journal_entry r WHERE r.organisation_id = e.organisation_id AND r.reverses_entry_id = e.id)
    ) THEN
      RAISE EXCEPTION 'Asset % is voided only with the reversal of every month of its depreciation', NEW.id
        USING ERRCODE = 'FY056', HINT = 'Void it through the application, which reverses its months in the same transaction (FR-FA-01, FR-GL-12).';
    END IF;
    RETURN NULL;
  END IF;
  IF NEW.type IN ('credit_application', 'customer_credit_application') AND NOT EXISTS (
    SELECT 1 FROM fyniq.journal_entry e
    WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.id::text AND e.source_version = NEW.current_version
  ) THEN
    IF EXISTS (
      SELECT 1 FROM fyniq.allocation a
      WHERE a.organisation_id = NEW.organisation_id AND a.source_id = NEW.id AND a.reverses_id IS NULL
        AND NOT EXISTS (SELECT 1 FROM fyniq.allocation r WHERE r.organisation_id = a.organisation_id AND r.reverses_id = a.id)
    ) THEN
      RAISE EXCEPTION 'Credit application % is reversed only with the reversal of its allocations', NEW.id
        USING ERRCODE = 'FY056', HINT = 'Void it through the application, which reverses its allocations in the same transaction (FR-AR-18).';
    END IF;
    RETURN NULL;
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.journal_entry e
    JOIN fyniq.journal_entry r ON r.organisation_id = e.organisation_id AND r.reverses_entry_id = e.id
    WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.id::text
      AND e.source_type NOT IN ('bill_release', 'invoice_release', 'bank_out_release', 'bank_in_release')
      AND e.source_version = NEW.current_version AND e.reverses_entry_id IS NULL
  ) THEN
    RAISE EXCEPTION 'Document % is reversed only with the reversal of its entry', NEW.id
      USING ERRCODE = 'FY056', HINT = 'Reverse it through the application, which posts the reversing entry in the same transaction (FR-GL-12).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

-- A release is audited with its asset's void, in the same transaction (AUD-01).
DROP TRIGGER require_audit ON fyniq.fixed_asset_source;
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.fixed_asset_source
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document', 'asset_id');

GRANT UPDATE (released) ON fyniq.fixed_asset_source TO fyniq_api;

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
                               ('fixed_asset', 'fixed_asset.void'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset')) NOT VALID;
