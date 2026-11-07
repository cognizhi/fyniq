-- The ACRA reconciliation (PRD-001 FR-EQ-26; p1-equity-register slice 8, task 9.1, design D11).
--
-- An acra_extract document holds ACRA's register of members as a business profile gives it: the profile's day and each
-- holder's shares of each class, typed in (the owner's decision of 7 Nov 2026), recorded with the profile's PDF by
-- whoever may post on the cap table. It posts nothing; the register is compared with the latest one at its day.
--
-- The database is the backstop (FY297): an extract keeps its own keys, its day, and a list of holdings, each of its own
-- keys, a holder marked shareholder or investor, an active share-capital class and whole shares above zero. The widened
-- check is added NOT VALID and validated by 000271.

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
                  'instrument_template', 'option_pool', 'option_grant', 'option_event', 'round_scenario', 'investor_right', 'controller_entry', 'acra_extract')) NOT VALID;

-- +goose StatementBegin
-- An ACRA extract keeps its rules whoever writes it (FY297).
CREATE FUNCTION fyniq.acra_extract_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t     text;
  p     jsonb := NEW.payload;
  extra text;
  h     jsonb;
BEGIN
  SELECT d.type INTO t FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id;
  IF t IS DISTINCT FROM 'acra_extract' THEN
    RETURN NEW;
  END IF;
  SELECT k INTO extra FROM jsonb_object_keys(p) k WHERE k NOT IN ('date', 'profile_on', 'holdings', 'note') LIMIT 1;
  IF extra IS NOT NULL OR coalesce(p->>'profile_on', '') = '' OR jsonb_typeof(p->'holdings') IS DISTINCT FROM 'array' THEN
    RAISE EXCEPTION 'An ACRA extract keeps its own keys: the business profile''s day and its holdings'
      USING ERRCODE = 'FY297', HINT = 'Type in the holdings the business profile lists (FR-EQ-26).';
  END IF;
  FOR h IN SELECT * FROM jsonb_array_elements(p->'holdings') LOOP
    SELECT k INTO extra FROM jsonb_object_keys(h) k WHERE k NOT IN ('holder_id', 'class_account_id', 'shares') LIMIT 1;
    IF extra IS NOT NULL OR NOT coalesce(h->>'shares', '') ~ '^[1-9][0-9]{0,13}$' THEN
      RAISE EXCEPTION 'An ACRA extract''s holding is a holder''s whole shares of a class' USING ERRCODE = 'FY297',
        HINT = 'Type the shares as the business profile lists them (FR-EQ-26).';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM fyniq.contact c
                    WHERE c.organisation_id = NEW.organisation_id AND c.id::text = h->>'holder_id'
                      AND (c.is_shareholder OR c.is_investor)) THEN
      RAISE EXCEPTION 'An ACRA extract''s holder is a contact marked shareholder or investor' USING ERRCODE = 'FY297',
        HINT = 'Mark the contact a shareholder in Contacts first (FR-EQ-15, FR-EQ-26).';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM fyniq.account a
                    WHERE a.organisation_id = NEW.organisation_id AND a.id::text = h->>'class_account_id'
                      AND a.role = 'share-capital' AND a.is_active AND NOT a.is_group) THEN
      RAISE EXCEPTION 'An ACRA extract''s class is an active share-capital account' USING ERRCODE = 'FY297',
        HINT = 'Choose 3010 Ordinary or 3020 Preference share capital (FR-EQ-26).';
    END IF;
  END LOOP;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER acra_extract_guard BEFORE INSERT ON fyniq.document_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.acra_extract_guard();
