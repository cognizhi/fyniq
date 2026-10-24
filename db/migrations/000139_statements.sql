-- Statements of account (PRD-001 FR-AR-08; p1-sales task 7.2).
--
-- A statement sent to a customer is a document of its own, statement, numbered from its own sequence (ST-) when it is
-- sent: its version keeps what the statement said (the period, the currency, the balance brought forward, the lines,
-- the closing balance and its ageing), its PDF is its evidence, and its email a send, as an invoice's are. It posts
-- nothing, as a purchase order or a quote does, and is never reversed: a statement sent stays as it was sent. The
-- email is of its own purpose, from the organisation's invoices sender. The widened checks are added NOT VALID and
-- validated by 000140 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation', 'bill', 'bill_payment', 'vendor_credit', 'credit_application',
                  'supplier_refund', 'purchase_order', 'invoice', 'customer_receipt', 'write_off', 'credit_note',
                  'customer_credit_application', 'customer_refund', 'customer_deposit', 'quote', 'sales_order', 'statement')) NOT VALID;

ALTER TABLE fyniq.document_sequence DROP CONSTRAINT document_sequence_type_known;
ALTER TABLE fyniq.document_sequence ADD CONSTRAINT document_sequence_type_known
  CHECK (document_type IN ('journal', 'bill', 'bill_payment', 'vendor_credit', 'supplier_refund', 'purchase_order', 'invoice',
                           'customer_receipt', 'write_off', 'credit_note', 'customer_refund', 'customer_deposit', 'quote',
                           'sales_order', 'statement')) NOT VALID;

ALTER TABLE fyniq.email_message DROP CONSTRAINT email_message_purpose_known;
ALTER TABLE fyniq.email_message ADD CONSTRAINT email_message_purpose_known
  CHECK (purpose IN ('test', 'sign_in_alert', 'notification', 'digest', 'invoice', 'quote', 'reminder', 'statement')) NOT VALID;

-- A statement posts nothing: no entry names one (FY204), and it is never reversed (FY204), for what was sent stays sent.
-- +goose StatementBegin
CREATE FUNCTION fyniq.statement_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.type = 'statement' AND NEW.status = 'reversed' THEN
    RAISE EXCEPTION 'Statement % was sent and stays as it was', NEW.id
      USING ERRCODE = 'FY204', HINT = 'Send a new statement instead (FR-AR-08).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER statement_guard BEFORE UPDATE OF status ON fyniq.document
  FOR EACH ROW WHEN (NEW.type = 'statement') EXECUTE FUNCTION fyniq.statement_guard();

-- +goose StatementBegin
CREATE FUNCTION fyniq.journal_entry_not_statement() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  -- Only a source that is a document's ID can name one; looked up by the document's key.
  IF NEW.source_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
    IF EXISTS (SELECT 1 FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.source_id::uuid
               AND d.type = 'statement') THEN
      RAISE EXCEPTION 'Entry % names statement % as its source', NEW.posting_identity, NEW.source_id
        USING ERRCODE = 'FY204', HINT = 'A statement is sent, not posted (FR-AR-08).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER journal_entry_not_statement BEFORE INSERT ON fyniq.journal_entry
  FOR EACH ROW EXECUTE FUNCTION fyniq.journal_entry_not_statement();

-- A send is of the current version of a sent quote, sales order, statement or issued invoice (FY196).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.document_send_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  doc fyniq.document;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The send % of document % never changes', OLD.id, OLD.document_id
      USING ERRCODE = 'FY195', HINT = 'Send the document again instead (FR-AR-06).';
  END IF;
  SELECT * INTO doc FROM fyniq.document WHERE organisation_id = NEW.organisation_id AND id = NEW.document_id;
  IF doc.type NOT IN ('quote', 'sales_order', 'invoice', 'statement') OR doc.status <> 'posted' OR doc.current_version <> NEW.document_version THEN
    RAISE EXCEPTION 'Only the current version of a sent quote or statement or an issued invoice is sent, not version % of a % that is %',
      NEW.document_version, doc.type, doc.status
      USING ERRCODE = 'FY196', HINT = 'Issue or send it first; a draft is never sent (FR-AR-06).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
