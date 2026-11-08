-- Feasibility studies (PRD-001 FR-PLN-06, FR-PLN-08; p1-planning slice 7, task 7.3, design D8).
--
-- A feasibility study document is one idea's inputs, as the workbook's sheet, and its date: its evaluation period,
-- discount rate and longest acceptable payback, its investment, depreciation life and salvage, its working capital,
-- and its units, price, variable and fixed costs with their yearly changes. It posts nothing and needs no recording: a study is drafted,
-- changed and kept. Its results are worked out on every read, with sg-cit's headline rate at the study's date.
--
-- The database is the backstop (FY302): a study keeps its own keys, a name of 1 to 80 characters, a date, 1 to 10
-- years, a life of 1 to 50 years, its figures as decimal strings (a rate and the working capital at most 1, a change above -1
-- and below 10, units to 1 decimal) and a salvage no more than its investment. The widened check is added NOT VALID
-- and validated by 000284.

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
                  'feasibility_study')) NOT VALID;

-- +goose StatementBegin
-- A feasibility study keeps its rules whoever writes it (FY302).
CREATE FUNCTION fyniq.feasibility_study_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t   text;
  p   jsonb := NEW.payload;
  bad text;
  k   text;
BEGIN
  SELECT d.type INTO t FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id;
  IF t IS DISTINCT FROM 'feasibility_study' THEN
    RETURN NEW;
  END IF;
  IF NOT fyniq.forecast_keys(p,
       ARRAY['name', 'date', 'years', 'discount_rate', 'max_payback', 'investment', 'life', 'salvage', 'working_capital', 'units',
             'unit_growth', 'price', 'price_growth', 'variable_cost', 'variable_cost_growth', 'fixed', 'fixed_growth'],
       ARRAY['name', 'date', 'years', 'discount_rate', 'max_payback', 'investment', 'life', 'salvage', 'working_capital', 'units',
             'unit_growth', 'price', 'price_growth', 'variable_cost', 'variable_cost_growth', 'fixed', 'fixed_growth']) THEN
    bad := 'its keys';
  ELSIF jsonb_typeof(p->'date') IS DISTINCT FROM 'string' OR p->>'date' !~ '^[0-9]{4}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])$' THEN
    bad := 'its date';
  ELSIF jsonb_typeof(p->'name') IS DISTINCT FROM 'string' OR char_length(p->>'name') NOT BETWEEN 1 AND 80 THEN
    bad := 'its name';
  ELSIF jsonb_typeof(p->'years') IS DISTINCT FROM 'number' OR (p->>'years') !~ '^[0-9]{1,2}$' OR (p->>'years')::int NOT BETWEEN 1 AND 10 THEN
    bad := 'its years';
  ELSIF jsonb_typeof(p->'life') IS DISTINCT FROM 'number' OR (p->>'life') !~ '^[0-9]{1,2}$' OR (p->>'life')::int NOT BETWEEN 1 AND 50 THEN
    bad := 'its life';
  END IF;
  IF bad IS NULL THEN
    FOREACH k IN ARRAY ARRAY['discount_rate', 'max_payback', 'investment', 'salvage', 'working_capital', 'units', 'price', 'variable_cost', 'fixed'] LOOP
      IF NOT fyniq.forecast_decimal(p->k, false) THEN
        bad := 'its ' || replace(k, '_', ' ');
        EXIT;
      END IF;
    END LOOP;
  END IF;
  IF bad IS NULL THEN
    FOREACH k IN ARRAY ARRAY['unit_growth', 'price_growth', 'variable_cost_growth', 'fixed_growth'] LOOP
      IF NOT fyniq.forecast_decimal(p->k, true) OR (p->>k)::numeric <= -1 OR (p->>k)::numeric >= 10 THEN
        bad := 'its ' || replace(k, '_', ' ');
        EXIT;
      END IF;
    END LOOP;
  END IF;
  IF bad IS NULL AND ((p->>'discount_rate')::numeric > 1 OR (p->>'working_capital')::numeric > 1) THEN
    bad := 'its rates';
  ELSIF bad IS NULL AND (p->>'max_payback')::numeric > 50 THEN
    bad := 'its longest payback';
  ELSIF bad IS NULL AND (p->>'units') !~ '^[0-9]+(\.[0-9])?$' THEN
    bad := 'its units';
  ELSIF bad IS NULL AND (p->>'salvage')::numeric > (p->>'investment')::numeric THEN
    bad := 'its salvage';
  END IF;
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'A feasibility study keeps its own keys and decimal strings: % does not', bad USING ERRCODE = 'FY302',
      HINT = 'Send the study as the API takes it: a name, 1 to 10 years, a life of 1 to 50 years, its figures as decimal strings, rates at most 1, changes above -1, and a salvage no more than the investment (FR-PLN-06).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER feasibility_study_guard BEFORE INSERT ON fyniq.document_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.feasibility_study_guard();
