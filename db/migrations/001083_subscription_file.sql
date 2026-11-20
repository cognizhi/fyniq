-- A subscription contract's evidence (PRD-001 FR-AR-15, FR-DOC-05; rev 0.586): the signed order form, the customer's email or the amendment letter are files on a document of their own,
-- `subscription_file`, one per contract, made with its first file and posted at once so every file is committed to the retention-locked store and is append-only (FR-DOC-13), as a
-- money account's bank agreements are (000156). It posts nothing and is never reversed (FYP24). The widened check is added NOT VALID and validated by 001084.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('account_merge', 'acra_extract', 'asset_disposal', 'bank_agreement', 'bank_transaction', 'bill',
                  'bill_payment', 'budget', 'cash_advance', 'claim_reimbursement', 'claimant_bank_change', 'controller_entry',
                  'corporate_detail', 'corporate_document', 'corporate_officer', 'credit_application', 'credit_note', 'customer_credit_application',
                  'customer_deposit', 'customer_receipt', 'customer_refund', 'director_confirmation', 'director_fee', 'director_pay_choice',
                  'director_repayment', 'director_settlement', 'equity_transaction', 'exempt_status', 'expense_claim', 'feasibility_study',
                  'fixed_asset', 'forecast', 'fx_revaluation', 'gst_forecast', 'holder_record', 'instrument',
                  'instrument_template', 'investor_right', 'invoice', 'journal', 'nominee_entry', 'opening_balance',
                  'option_event', 'option_grant', 'option_pool', 'owner_succession', 'payee_bank_change', 'payment_batch',
                  'pre_incorporation_cost', 'purchase_order', 'quote', 'round_scenario', 'sales_order', 'share_class',
                  'statement', 'statements_approval', 'statutory_filing', 'subscription_file', 'supplier_refund', 'vendor_credit',
                  'write_off')) NOT VALID;

-- +goose StatementBegin
CREATE FUNCTION fyniq.subscription_file_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.status = 'reversed' THEN
    RAISE EXCEPTION 'The files of subscription contract % stay as they were', NEW.id
      USING ERRCODE = 'FYP24', HINT = 'Add a newer file instead: evidence is append-only (FR-DOC-13).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER subscription_file_guard BEFORE UPDATE OF status ON fyniq.document
  FOR EACH ROW WHEN (NEW.type = 'subscription_file') EXECUTE FUNCTION fyniq.subscription_file_guard();
