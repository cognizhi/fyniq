-- A money account's bank agreements, as evidence (PRD-001 FR-DOC-05, FR-BNK-17; p1-bank-import-reconcile task 8.5,
-- design D13): the agreements of a source switch, such as DBS's for RAPID, are files on a document of their own,
-- `bank_agreement`, one per money account, posted when it is made so its files are committed to the retention-locked
-- store at once and are append-only (FR-DOC-13). It posts nothing: no entry names one, and it is never reversed (FY231).
-- The widened check is added NOT VALID and validated by 000157 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation', 'bill', 'bill_payment', 'vendor_credit', 'credit_application',
                  'supplier_refund', 'purchase_order', 'invoice', 'customer_receipt', 'write_off', 'credit_note',
                  'customer_credit_application', 'customer_refund', 'customer_deposit', 'quote', 'sales_order', 'statement',
                  'bank_transaction', 'bank_agreement')) NOT VALID;

-- +goose StatementBegin
CREATE FUNCTION fyniq.bank_agreement_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.status = 'reversed' THEN
    RAISE EXCEPTION 'The bank agreements of % stay as they were', NEW.id
      USING ERRCODE = 'FY231', HINT = 'Add a newer file instead: evidence is append-only (FR-DOC-13).';
  END IF;
  RETURN NEW;
END
$$;

CREATE FUNCTION fyniq.journal_entry_not_bank_agreement() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  -- Only a source that is a document's ID can name one; looked up by the document's key.
  IF NEW.source_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
    IF EXISTS (SELECT 1 FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.source_id::uuid
               AND d.type = 'bank_agreement') THEN
      RAISE EXCEPTION 'Entry % names bank agreements % as its source', NEW.posting_identity, NEW.source_id
        USING ERRCODE = 'FY231', HINT = 'Bank agreements are evidence, not a posting (FR-DOC-05).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bank_agreement_guard BEFORE UPDATE OF status ON fyniq.document
  FOR EACH ROW WHEN (NEW.type = 'bank_agreement') EXECUTE FUNCTION fyniq.bank_agreement_guard();
CREATE TRIGGER journal_entry_not_bank_agreement BEFORE INSERT ON fyniq.journal_entry
  FOR EACH ROW EXECUTE FUNCTION fyniq.journal_entry_not_bank_agreement();
