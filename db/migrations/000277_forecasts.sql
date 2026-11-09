-- Driver-based forecasts (PRD-001 FR-PLN-02, FR-PLN-03, FR-PLN-08; p1-planning slice 3, task 3.1, design D5).
--
-- A forecast document is a plan: its drivers, its headcount, capex, funding and other-expense rows, its tax inputs and
-- its scenarios. It posts nothing (FR-PLN-08); a recorded one changes no more, and the latest recorded is the company's
-- own. Its results are worked out on every read and never stored.
--
-- The database is the backstop (FY300): a forecast keeps its own keys, at the top and in every row, a name of 1 to 80
-- characters, a horizon of 12 to 60 months, every amount and rate as a decimal string, and its rows and scenarios
-- within their limits. The widened check is added NOT VALID and validated by 000278.

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
                  'controller_entry', 'acra_extract', 'gst_forecast', 'statutory_filing', 'forecast')) NOT VALID;

-- +goose StatementBegin
-- An object's keys are among allowed and include required; a key missing from allowed is refused.
CREATE FUNCTION fyniq.forecast_keys(o jsonb, allowed text[], required text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT jsonb_typeof(o) = 'object'
     AND NOT EXISTS (SELECT 1 FROM jsonb_object_keys(o) k WHERE k <> ALL (allowed))
     AND NOT EXISTS (SELECT 1 FROM unnest(required) r WHERE NOT o ? r)
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- A value is a decimal string of zero or more, signed when signed is true.
CREATE FUNCTION fyniq.forecast_decimal(v jsonb, signed boolean) RETURNS boolean
LANGUAGE sql IMMUTABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT jsonb_typeof(v) = 'string'
     AND (v #>> '{}') ~ CASE WHEN signed THEN '^-?(0|[1-9][0-9]{0,14})(\.[0-9]{1,6})?$' ELSE '^(0|[1-9][0-9]{0,14})(\.[0-9]{1,6})?$' END
$$;
-- +goose StatementEnd

-- The guard runs as whoever writes a version: the roles that write documents may call its helpers, no one else.
REVOKE ALL ON FUNCTION fyniq.forecast_keys(jsonb, text[], text[]), fyniq.forecast_decimal(jsonb, boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.forecast_keys(jsonb, text[], text[]), fyniq.forecast_decimal(jsonb, boolean) TO fyniq_api, fyniq_worker;

-- +goose StatementBegin
-- A forecast keeps its rules whoever writes it (FY300).
CREATE FUNCTION fyniq.forecast_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t   text;
  p   jsonb := NEW.payload;
  bad text;
  x   jsonb;
  k   text;
BEGIN
  SELECT d.type INTO t FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id;
  IF t IS DISTINCT FROM 'forecast' THEN
    RETURN NEW;
  END IF;
  IF NOT fyniq.forecast_keys(p,
       ARRAY['name', 'horizon', 'last_actual', 'cash_adjustment', 'buffer_months', 'revenue', 'costs', 'opex', 'headcount',
             'capex', 'funding', 'tax', 'scenarios'],
       ARRAY['name', 'horizon', 'cash_adjustment', 'revenue', 'costs', 'opex', 'headcount', 'capex', 'funding', 'tax', 'scenarios']) THEN
    bad := 'its keys';
  ELSIF jsonb_typeof(p->'name') IS DISTINCT FROM 'string' OR char_length(p->>'name') NOT BETWEEN 1 AND 80 THEN
    bad := 'its name';
  ELSIF jsonb_typeof(p->'horizon') IS DISTINCT FROM 'number' OR (p->>'horizon') !~ '^[0-9]{1,2}$'
     OR (p->>'horizon')::int NOT BETWEEN 12 AND 60 THEN
    bad := 'its horizon';
  ELSIF p ? 'last_actual' AND (jsonb_typeof(p->'last_actual') IS DISTINCT FROM 'string' OR p->>'last_actual' !~ '^[0-9]{4}-(0[1-9]|1[0-2])$') THEN
    bad := 'its last actual month';
  ELSIF NOT fyniq.forecast_decimal(p->'cash_adjustment', true) THEN
    bad := 'its cash adjustment';
  ELSIF p ? 'buffer_months' AND (jsonb_typeof(p->'buffer_months') IS DISTINCT FROM 'number' OR (p->>'buffer_months') !~ '^[0-9]{1,2}$'
     OR (p->>'buffer_months')::int > 36) THEN
    bad := 'its buffer';
  ELSIF NOT fyniq.forecast_keys(p->'revenue',
       ARRAY['customers', 'new_customers', 'new_growth', 'churn', 'arpa', 'price_increase', 'services', 'services_growth', 'other', 'collection'],
       ARRAY['customers', 'new_customers', 'new_growth', 'churn', 'arpa', 'price_increase', 'services', 'services_growth', 'other', 'collection'])
     OR EXISTS (SELECT 1 FROM jsonb_each(p->'revenue') e WHERE e.key <> 'collection' AND NOT fyniq.forecast_decimal(e.value, false))
     OR NOT fyniq.forecast_keys(p->'revenue'->'collection', ARRAY['this_month', 'next_month', 'month_after'],
       ARRAY['this_month', 'next_month', 'month_after'])
     OR EXISTS (SELECT 1 FROM jsonb_each(p->'revenue'->'collection') c WHERE NOT fyniq.forecast_decimal(c.value, false)) THEN
    bad := 'its revenue drivers';
  ELSIF NOT fyniq.forecast_keys(p->'costs', ARRAY['hosting', 'ai', 'contractors', 'fees', 'salary_increase'],
       ARRAY['hosting', 'ai', 'contractors', 'fees', 'salary_increase'])
     OR EXISTS (SELECT 1 FROM jsonb_each(p->'costs') e WHERE NOT fyniq.forecast_decimal(e.value, false)) THEN
    bad := 'its cost drivers';
  ELSIF NOT fyniq.forecast_keys(p->'tax', ARRAY['start_up_years', 'losses_brought_forward', 'payment_lag'],
       ARRAY['start_up_years', 'losses_brought_forward', 'payment_lag'])
     OR (p->'tax'->>'start_up_years') !~ '^[0-3]$' OR (p->'tax'->>'payment_lag') !~ '^[0-9]{1,2}$'
     OR (p->'tax'->>'payment_lag')::int > 24 OR NOT fyniq.forecast_decimal(p->'tax'->'losses_brought_forward', false) THEN
    bad := 'its tax inputs';
  END IF;
  IF bad IS NULL THEN
    FOREACH k IN ARRAY ARRAY['opex', 'headcount', 'capex', 'funding', 'scenarios'] LOOP
      IF jsonb_typeof(p->k) IS DISTINCT FROM 'array'
         OR jsonb_array_length(p->k) > (CASE k WHEN 'opex' THEN 30 WHEN 'headcount' THEN 100 WHEN 'scenarios' THEN 10 ELSE 50 END)
         OR (k = 'scenarios' AND jsonb_array_length(p->k) = 0) THEN
        bad := 'its ' || k;
        EXIT;
      END IF;
    END LOOP;
  END IF;
  IF bad IS NULL THEN
    FOR x IN SELECT * FROM jsonb_array_elements(p->'opex') LOOP
      IF NOT fyniq.forecast_keys(x, ARRAY['label', 'report_group', 'base', 'growth', 'percent'], ARRAY['label', 'base', 'growth', 'percent'])
         OR jsonb_typeof(x->'label') IS DISTINCT FROM 'string' OR char_length(x->>'label') NOT BETWEEN 1 AND 80
         OR NOT (fyniq.forecast_decimal(x->'base', false) AND fyniq.forecast_decimal(x->'growth', false) AND fyniq.forecast_decimal(x->'percent', false)) THEN
        bad := 'an expense line';
      END IF;
    END LOOP;
    FOR x IN SELECT * FROM jsonb_array_elements(p->'headcount') LOOP
      IF NOT fyniq.forecast_keys(x, ARRAY['role', 'function', 'start', 'end', 'salary', 'cpf'], ARRAY['function', 'start', 'salary', 'cpf'])
         OR x->>'function' NOT IN ('COGS', 'R&D', 'S&M', 'G&A') OR x->>'start' !~ '^[0-9]{4}-(0[1-9]|1[0-2])$'
         OR (x ? 'end' AND x->>'end' !~ '^[0-9]{4}-(0[1-9]|1[0-2])$') OR jsonb_typeof(x->'cpf') IS DISTINCT FROM 'boolean'
         OR NOT fyniq.forecast_decimal(x->'salary', false) THEN
        bad := 'a headcount row';
      END IF;
    END LOOP;
    FOR x IN SELECT * FROM jsonb_array_elements(p->'capex') LOOP
      IF NOT fyniq.forecast_keys(x, ARRAY['item', 'amount', 'month', 'life'], ARRAY['amount', 'month', 'life'])
         OR x->>'month' !~ '^[0-9]{4}-(0[1-9]|1[0-2])$' OR (x->>'life') !~ '^[0-9]{1,3}$' OR (x->>'life')::int NOT BETWEEN 1 AND 600
         OR NOT fyniq.forecast_decimal(x->'amount', false) THEN
        bad := 'a capex row';
      END IF;
    END LOOP;
    FOR x IN SELECT * FROM jsonb_array_elements(p->'funding') LOOP
      IF NOT fyniq.forecast_keys(x, ARRAY['type', 'month', 'amount', 'rate', 'tenor', 'notes'], ARRAY['type', 'month', 'amount'])
         OR x->>'type' NOT IN ('Equity', 'SAFE', 'Loan', 'Grant') OR x->>'month' !~ '^[0-9]{4}-(0[1-9]|1[0-2])$'
         OR NOT fyniq.forecast_decimal(x->'amount', false)
         OR (x ? 'rate' AND NOT fyniq.forecast_decimal(x->'rate', false))
         OR (x ? 'tenor' AND ((x->>'tenor') !~ '^[0-9]{1,3}$' OR (x->>'tenor')::int > 360)) THEN
        bad := 'a funding row';
      END IF;
    END LOOP;
    FOR x IN SELECT * FROM jsonb_array_elements(p->'scenarios') LOOP
      IF NOT fyniq.forecast_keys(x, ARRAY['name', 'levers', 'buffer_months'], ARRAY['name', 'levers'])
         OR jsonb_typeof(x->'name') IS DISTINCT FROM 'string' OR char_length(x->>'name') NOT BETWEEN 1 AND 40
         OR NOT fyniq.forecast_keys(x->'levers',
              ARRAY['new_customers', 'churn', 'price', 'services', 'opex', 'direct_costs', 'fund_amount', 'hire_delay', 'fund_delay'],
              ARRAY['new_customers', 'churn', 'price', 'services', 'opex', 'direct_costs', 'fund_amount', 'hire_delay', 'fund_delay'])
         OR EXISTS (SELECT 1 FROM jsonb_each(x->'levers') e WHERE e.key NOT IN ('hire_delay', 'fund_delay') AND NOT fyniq.forecast_decimal(e.value, false))
         OR (x->'levers'->>'hire_delay') !~ '^[0-9]{1,2}$' OR (x->'levers'->>'fund_delay') !~ '^[0-9]{1,2}$'
         OR (x->'levers'->>'hire_delay')::int > 24 OR (x->'levers'->>'fund_delay')::int > 24
         OR (x ? 'buffer_months' AND ((x->>'buffer_months') !~ '^[0-9]{1,2}$' OR (x->>'buffer_months')::int > 36)) THEN
        bad := 'a scenario';
      END IF;
    END LOOP;
  END IF;
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'A forecast keeps its own keys and decimal strings: % does not', bad USING ERRCODE = 'FY300',
      HINT = 'Send the forecast as the API takes it: a name, a horizon of 12 to 60 months, its drivers as decimal strings, and at most 30 expense lines, 100 people, 50 purchases, 50 funding rows and 10 scenarios (FR-PLN-02).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER forecast_guard BEFORE INSERT ON fyniq.document_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.forecast_guard();
