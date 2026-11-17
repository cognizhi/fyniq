-- Corporate documents (PRD-001 FR-CS-05; p1-company-secretary task 4.1, design D6).
--
-- A corporate_document document is a resolution, a notice or minutes drafted from one of Fyniq's templates and filled
-- from the records on the day it was drafted: its kind, the template's kind and version, the fields as asked and the
-- paper as filled. It is a draft to review and have signed: Fyniq never signs or files it. Recorded with its signed copy
-- on another person's approval (task 4.2, which adds that operation), it is an entry of the minute book; a recorded one
-- never changes. It never posts.
--
-- The database is the backstop: a corporate document keeps its own keys, a known kind, a template named for its kind
-- with a version, its fields and its paper as JSON objects, a note of at most 1,000 characters, and no NRIC or FIN in full
-- anywhere in it (FY356). The widened check is added NOT VALID and validated by 000400.

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
                  'instrument_template', 'option_pool', 'option_grant', 'option_event', 'round_scenario', 'investor_right',
                  'controller_entry', 'acra_extract', 'gst_forecast', 'statutory_filing', 'forecast', 'budget',
                  'feasibility_study', 'corporate_detail', 'corporate_officer',
                  'nominee_entry', 'statements_approval', 'corporate_document')) NOT VALID;

-- +goose StatementBegin
-- A corporate document keeps its rules whoever writes it (FY356).
CREATE FUNCTION fyniq.corporate_document_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t     text;
  p     jsonb := NEW.payload;
  extra text;
BEGIN
  SELECT d.type INTO t FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id;
  IF t = 'corporate_document' THEN
    SELECT k INTO extra FROM jsonb_object_keys(p) k
     WHERE k NOT IN ('date', 'kind', 'template', 'fields', 'paper', 'signed_on', 'proposal_id', 'note') LIMIT 1;
    IF extra IS NOT NULL OR coalesce(p->>'date', '') = ''
       OR NOT coalesce(p->>'kind', '') IN ('directors.approve_statements', 'directors.allot_shares', 'directors.appoint_officer',
                                           'directors.remove_officer', 'directors.change_office', 'members.written_resolution',
                                           'agm.notice', 'agm.minutes', 'agm.dispensation')
       OR coalesce(p->>'template', '') !~ ('^' || replace(p->>'kind', '.', '\.') || '@[1-9][0-9]*$')
       OR jsonb_typeof(p->'fields') IS DISTINCT FROM 'object' OR jsonb_typeof(p->'paper') IS DISTINCT FROM 'object'
       OR length(coalesce(p->>'note', '')) > 1000 THEN
      RAISE EXCEPTION 'A corporate document keeps its own keys: a known kind, its template and version, its fields and its paper'
        USING ERRCODE = 'FY356', HINT = 'Draft it from one of Fyniq''s templates (FR-CS-05).';
    END IF;
    IF p ? 'signed_on' AND (p->>'signed_on')::date < (p->>'date')::date THEN
      RAISE EXCEPTION 'A corporate document is signed on or after the day it was drafted' USING ERRCODE = 'FY356',
        HINT = 'Enter the day it was signed (FR-CS-05).';
    END IF;
    IF p::text ~ '\m[STFGMstfgm][0-9]{7}[A-Za-z]\M' THEN
      RAISE EXCEPTION 'A corporate document never shows an NRIC or FIN in full' USING ERRCODE = 'FY356',
        HINT = 'Leave identifiers out of the paper (SEC-15, CMP-PD-07).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER corporate_document_guard BEFORE INSERT ON fyniq.document_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.corporate_document_guard();
