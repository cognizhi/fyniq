-- The statutory calendar's filing records (PRD-001 FR-TAX-07, CMP-02; p1-gst slice 3a, task 4.2, design D6).
--
-- A statutory_filing document records what a person did about an item of the calendar: filed on a day with its
-- reference (the acknowledgement attached as evidence), or not required with the reason, such as the ECI waiver; or the
-- withdrawal of an earlier record, which is reversed beside it. Records post nothing. The calendar's items themselves
-- are generated from the sg-deadlines pack each time, never stored.
--
-- The database is the backstop (FY299): a record keeps its own keys, an item's key, and the fields of its outcome; a
-- withdrawal names a record of the same item, reversed; and an item has at most one live record (filed or not required,
-- and not reversed), however many requests race, as posting one takes a lock on the item's key. A reversed record posts
-- nothing to reverse. The widened check is added NOT VALID and validated by 000275.

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
                  'controller_entry', 'acra_extract', 'gst_forecast', 'statutory_filing')) NOT VALID;

-- +goose StatementBegin
-- A filing record keeps its rules whoever writes it (FY299).
CREATE FUNCTION fyniq.statutory_filing_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t     text;
  p     jsonb := NEW.payload;
  extra text;
  bad   text;
BEGIN
  SELECT d.type INTO t FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id;
  IF t IS DISTINCT FROM 'statutory_filing' THEN
    RETURN NEW;
  END IF;
  SELECT k INTO extra FROM jsonb_object_keys(p) k
   WHERE k NOT IN ('date', 'key', 'outcome', 'filed_on', 'reference', 'reason', 'note', 'withdraws') LIMIT 1;
  IF extra IS NOT NULL OR coalesce(p->>'date', '') !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'
     OR coalesce(p->>'key', '') !~ '^[a-z][a-z0-9_]{1,40}:[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN
    bad := 'its own keys, its day and an item''s key';
  ELSIF p->>'outcome' = 'filed' THEN
    IF coalesce(p->>'filed_on', '') !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' OR char_length(coalesce(p->>'reference', '')) NOT BETWEEN 1 AND 100
       OR p ? 'reason' OR p ? 'withdraws' THEN
      bad := 'a filed record''s day and reference of 1 to 100 characters, and no reason';
    END IF;
  ELSIF p->>'outcome' = 'not_required' THEN
    IF char_length(coalesce(p->>'reason', '')) NOT BETWEEN 1 AND 500 OR p ? 'filed_on' OR p ? 'reference' OR p ? 'withdraws' THEN
      bad := 'a record not required''s reason of 1 to 500 characters, and no day or reference';
    END IF;
  ELSIF p->>'outcome' = 'withdrawn' THEN
    IF char_length(coalesce(p->>'reason', '')) NOT BETWEEN 1 AND 500 OR p ? 'filed_on' OR p ? 'reference'
       OR NOT EXISTS (SELECT 1 FROM fyniq.document d JOIN fyniq.document_version v
                        ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
                       WHERE d.organisation_id = NEW.organisation_id AND d.type = 'statutory_filing' AND d.id::text = p->>'withdraws'
                         AND d.id <> NEW.document_id AND v.payload->>'key' = p->>'key' AND v.payload->>'outcome' IN ('filed', 'not_required')) THEN
      bad := 'a withdrawal''s reason, and a record of the same item it withdraws';
    END IF;
  ELSE
    bad := 'an outcome: filed, not_required or withdrawn';
  END IF;
  IF bad IS NOT NULL OR char_length(coalesce(p->>'note', '')) > 1000 THEN
    RAISE EXCEPTION 'A filing record keeps %', coalesce(bad, 'a note of at most 1,000 characters')
      USING ERRCODE = 'FY299', HINT = 'Record the item the calendar gives, as filed with its day and reference, or as not required with the reason (FR-TAX-07).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER statutory_filing_guard BEFORE INSERT ON fyniq.document_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.statutory_filing_guard();

-- +goose StatementBegin
-- An item has at most one live record, and a withdrawal withdraws a reversed record once (FY299). Posting a record takes
-- a lock on its item's key until commit, so two requests for one item never both see it free.
CREATE FUNCTION fyniq.statutory_filing_live() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  p jsonb;
BEGIN
  SELECT v.payload INTO p FROM fyniq.document_version v
   WHERE v.organisation_id = NEW.organisation_id AND v.document_id = NEW.id AND v.version = NEW.current_version;
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.organisation_id::text || ':' || coalesce(p->>'key', ''), 299));
  IF p->>'outcome' IN ('filed', 'not_required') AND EXISTS (
    SELECT 1 FROM fyniq.document d JOIN fyniq.document_version v
      ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
     WHERE d.organisation_id = NEW.organisation_id AND d.type = 'statutory_filing' AND d.status = 'posted' AND d.id <> NEW.id
       AND v.payload->>'key' = p->>'key' AND v.payload->>'outcome' IN ('filed', 'not_required')) THEN
    RAISE EXCEPTION 'The item % has a live record already', p->>'key'
      USING ERRCODE = 'FY299', HINT = 'Withdraw that record first, with the reason, then record the item again (FR-TAX-07).';
  END IF;
  IF p->>'outcome' = 'withdrawn' AND (NOT EXISTS (
      SELECT 1 FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id::text = p->>'withdraws' AND d.status = 'reversed')
    OR EXISTS (
      SELECT 1 FROM fyniq.document d JOIN fyniq.document_version v
        ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
       WHERE d.organisation_id = NEW.organisation_id AND d.type = 'statutory_filing' AND d.status = 'posted' AND d.id <> NEW.id
         AND v.payload->>'outcome' = 'withdrawn' AND v.payload->>'withdraws' = p->>'withdraws')) THEN
    RAISE EXCEPTION 'A withdrawal withdraws a reversed record of its item, once' USING ERRCODE = 'FY299',
      HINT = 'Withdraw a live record through the application, which reverses it in the same transaction (FR-TAX-07).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER statutory_filing_live BEFORE UPDATE OF status ON fyniq.document
  FOR EACH ROW WHEN (NEW.type = 'statutory_filing' AND NEW.status = 'posted' AND OLD.status = 'draft')
  EXECUTE FUNCTION fyniq.statutory_filing_live();

-- A filing record posts nothing, so withdrawing it reverses nothing.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.document_reversed_posted() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.type IN ('purchase_order', 'quote', 'sales_order', 'statutory_filing') THEN
    RETURN NULL; -- a purchase order, a quote, a sales order or a filing record posts nothing, so closing, cancelling or withdrawing it reverses nothing
  END IF;
  IF NEW.type = 'payment_batch' THEN -- a batch posts nothing; once released, its payments are voided on their own
    IF EXISTS (SELECT 1 FROM fyniq.payment_batch_release r WHERE r.organisation_id = NEW.organisation_id AND r.batch_id = NEW.id) THEN
      RAISE EXCEPTION 'Batch % is released, so it is never cancelled', NEW.id USING ERRCODE = 'FY239',
        HINT = 'Void a payment the bank rejected on its own (FR-BNK-12).';
    END IF;
    RETURN NULL;
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
