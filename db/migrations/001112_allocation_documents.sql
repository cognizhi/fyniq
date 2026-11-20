-- Allocation rules and shares for services (PRD-001 FR-DIM-03, FR-EQ-13; p2-reporting-planning tasks 8.2 and 8.3, rev 0.609).
--
-- Four kinds of document. An allocation rule (the source expense accounts, the key and the targets) and a grant of shares for services (its fair value per share, service start and
-- tranches) are saved as documents of their own, a new version at each change, and post nothing. A run of a rule over whole months, and a month of a grant's expense, are each
-- saved with the draft journal they proposed and posted at once, so they are never changed or reversed (FYR14); the journal itself posts like any other, on an approver's say.
-- The widened check is added NOT VALID and validated by 001113.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('account_merge', 'acra_extract', 'allocation_rule', 'allocation_run', 'asset_disposal', 'bank_agreement', 'bank_transaction', 'bill',
                  'bill_payment', 'board_pack_pdf', 'budget', 'cash_advance', 'claim_reimbursement', 'claimant_bank_change', 'controller_entry',
                  'corporate_detail', 'corporate_document', 'corporate_officer', 'credit_application', 'credit_note', 'customer_credit_application',
                  'customer_deposit', 'customer_receipt', 'customer_refund', 'director_confirmation', 'director_fee', 'director_pay_choice',
                  'director_repayment', 'director_settlement', 'equity_transaction', 'exempt_status', 'expense_claim', 'feasibility_study',
                  'fixed_asset', 'forecast', 'fx_revaluation', 'gst_forecast', 'holder_record', 'instrument',
                  'instrument_template', 'investor_right', 'invoice', 'journal', 'nominee_entry', 'opening_balance',
                  'option_event', 'option_grant', 'option_pool', 'owner_succession', 'payee_bank_change', 'payment_batch',
                  'pre_incorporation_cost', 'purchase_order', 'quote', 'round_scenario', 'sales_order', 'share_class', 'share_grant', 'share_grant_month',
                  'statement', 'statements_approval', 'statutory_filing', 'subscription_file', 'supplier_refund', 'vendor_credit',
                  'write_off')) NOT VALID;

-- +goose StatementBegin
CREATE FUNCTION fyniq.proposal_run_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.status = 'reversed' OR (TG_OP = 'UPDATE' AND OLD.status = 'posted' AND NEW.status <> 'posted') THEN
    RAISE EXCEPTION 'A recorded run of % stays as it was', NEW.type
      USING ERRCODE = 'FYR14', HINT = 'Reverse its journal instead: the record of what was proposed is append-only (FR-DIM-03, FR-EQ-13).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER proposal_run_guard BEFORE UPDATE OF status ON fyniq.document
  FOR EACH ROW WHEN (NEW.type IN ('allocation_run', 'share_grant_month')) EXECUTE FUNCTION fyniq.proposal_run_guard();
