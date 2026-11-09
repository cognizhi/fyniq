-- The GST registration monitor's forecast (PRD-001 FR-TAX-05; p1-gst slice 2, task 3.2, design D4).
--
-- A gst_forecast document is a forecast of the taxable supplies of the 12 months from a day: its value in Singapore
-- dollars and its basis, recorded with its evidence (signed contracts or confirmed orders, as IRAS asks) by whoever
-- prepares GST returns. It posts nothing; the latest recorded is the prospective test's (the owner's decision of 7 Nov
-- 2026, PRD-001 rev 0.208).
--
-- The database is the backstop (FY298): a forecast keeps its own keys, its day, a value above zero with cents and a
-- basis of at most 1,000 characters. The widened check is added NOT VALID and validated by 000273.

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
                  'controller_entry', 'acra_extract', 'gst_forecast')) NOT VALID;

-- +goose StatementBegin
-- A GST forecast keeps its rules whoever writes it (FY298).
CREATE FUNCTION fyniq.gst_forecast_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t     text;
  p     jsonb := NEW.payload;
  extra text;
BEGIN
  SELECT d.type INTO t FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id;
  IF t IS DISTINCT FROM 'gst_forecast' THEN
    RETURN NEW;
  END IF;
  SELECT k INTO extra FROM jsonb_object_keys(p) k WHERE k NOT IN ('date', 'forecast_on', 'value', 'basis') LIMIT 1;
  IF extra IS NOT NULL OR coalesce(p->>'forecast_on', '') !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'
     OR coalesce(p->>'date', '') !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN
    RAISE EXCEPTION 'A GST forecast keeps its own keys: its day, its value and its basis'
      USING ERRCODE = 'FY298', HINT = 'Enter the day of the forecast and its value (FR-TAX-05).';
  END IF;
  -- The value is read in steps, so no key list or number is taken of what is not an object or a decimal string.
  IF jsonb_typeof(p->'value') IS DISTINCT FROM 'object' THEN
    extra := 'value';
  ELSIF EXISTS (SELECT 1 FROM jsonb_object_keys(p->'value') k WHERE k NOT IN ('amount', 'currency'))
     OR jsonb_typeof(p->'value'->'amount') IS DISTINCT FROM 'string' OR p->'value'->>'currency' IS DISTINCT FROM 'SGD'
     OR NOT coalesce(p->'value'->>'amount', '') ~ '^[0-9]{1,15}\.[0-9]{2}$' THEN
    extra := 'value';
  ELSIF (p->'value'->>'amount')::numeric <= 0 THEN
    extra := 'value';
  END IF;
  IF extra IS NOT NULL THEN
    RAISE EXCEPTION 'A GST forecast''s value is Singapore dollars above zero, with cents' USING ERRCODE = 'FY298',
      HINT = 'Enter the taxable supplies expected in the 12 months, such as 1200000.00 SGD (FR-TAX-05).';
  END IF;
  IF jsonb_typeof(p->'basis') IS DISTINCT FROM 'string' OR char_length(p->>'basis') NOT BETWEEN 1 AND 1000 THEN
    RAISE EXCEPTION 'A GST forecast''s basis is 1 to 1,000 characters' USING ERRCODE = 'FY298',
      HINT = 'Say what the forecast rests on, such as the signed contracts (FR-TAX-05).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER gst_forecast_guard BEFORE INSERT ON fyniq.document_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.gst_forecast_guard();
