-- A claim of GST bad debt relief (PRD-001 FR-AR-11, rev 0.608; p2-tax-filings task 3.2): a document of its own, `bad_debt_relief`, made and posted at once by the claim's
-- person, naming one posted bad-debt write-off, the GST it claims (RND-BDR) and the F5 period it goes in, with the inputs the rule read, so the entry replays (ENG-06). It posts
-- Dr the GST control account, Cr the account the write-off took the debt to (the rule bad_debt_relief@1), is never edited, and is undone by a reversal. Evidence (the debtor's
-- insolvency, the steps taken to recover the debt) attaches to it as to any document (FR-DOC-05). The widened check is added NOT VALID and validated by 001121.
--
-- Two backstops hold whatever the application does: a write-off is claimed once while a claim of it is live (a posted one), and a write-off with a live claim cannot be reversed
-- (void the claim first), so the GST control account never holds a relief for a debt that was reinstated. Neither adds a table or a grant: the claim is a document, and the
-- API role already posts documents.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('account_merge', 'acra_extract', 'asset_disposal', 'bad_debt_relief', 'bank_agreement', 'bank_transaction', 'bill',
                  'bill_payment', 'board_pack_pdf', 'budget', 'cash_advance', 'claim_reimbursement', 'claimant_bank_change', 'controller_entry',
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
CREATE FUNCTION fyniq.bad_debt_relief_posts() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  wo     text;
  woid   uuid;
  w      fyniq.document;
BEGIN
  SELECT v.payload ->> 'write_off_id' INTO wo FROM fyniq.document_version v
   WHERE v.organisation_id = NEW.organisation_id AND v.document_id = NEW.id AND v.version = NEW.current_version;
  BEGIN
    woid := wo::uuid;
  EXCEPTION WHEN invalid_text_representation OR null_value_not_allowed THEN
    RAISE EXCEPTION 'A claim of bad debt relief names the write-off it is for' USING ERRCODE = 'FYT01',
      HINT = 'Claim from a posted bad-debt write-off (FR-AR-11).';
  END;
  -- one claim at a time per write-off, so the check below sees the other that is posting
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.organisation_id::text || ':bdr:' || woid::text, 0));
  SELECT * INTO w FROM fyniq.document WHERE organisation_id = NEW.organisation_id AND id = woid;
  IF NOT FOUND OR w.type <> 'write_off' OR w.status <> 'posted' THEN
    RAISE EXCEPTION 'GST bad debt relief is claimed for a posted write-off, which % is not', woid USING ERRCODE = 'FYT01',
      HINT = 'Post the write-off first, or choose another (FR-AR-11).';
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.document d JOIN fyniq.document_version v
               ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
              WHERE d.organisation_id = NEW.organisation_id AND d.type = 'bad_debt_relief' AND d.status = 'posted' AND d.id <> NEW.id
                AND v.payload ->> 'write_off_id' = woid::text) THEN
    RAISE EXCEPTION 'Write-off % is claimed already', woid USING ERRCODE = 'FYT01',
      HINT = 'Void the earlier claim to claim again (FR-AR-11).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bad_debt_relief_posts BEFORE UPDATE OF status ON fyniq.document
  FOR EACH ROW WHEN (NEW.type = 'bad_debt_relief' AND NEW.status = 'posted' AND OLD.status IS DISTINCT FROM 'posted') EXECUTE FUNCTION fyniq.bad_debt_relief_posts();

-- +goose StatementBegin
CREATE FUNCTION fyniq.write_off_with_relief_stays() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended(OLD.organisation_id::text || ':bdr:' || OLD.id::text, 0));
  IF EXISTS (SELECT 1 FROM fyniq.document d JOIN fyniq.document_version v
               ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
              WHERE d.organisation_id = OLD.organisation_id AND d.type = 'bad_debt_relief' AND d.status = 'posted'
                AND v.payload ->> 'write_off_id' = OLD.id::text) THEN
    RAISE EXCEPTION 'Write-off % has GST bad debt relief claimed on it', OLD.id USING ERRCODE = 'FYT02',
      HINT = 'Void the claim of relief first, then the write-off (FR-AR-11).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER write_off_with_relief_stays BEFORE UPDATE OF status ON fyniq.document
  FOR EACH ROW WHEN (OLD.type = 'write_off' AND NEW.status = 'reversed' AND OLD.status IS DISTINCT FROM 'reversed') EXECUTE FUNCTION fyniq.write_off_with_relief_stays();
