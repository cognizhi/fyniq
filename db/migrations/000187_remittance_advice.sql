-- Remittance advice for a payment batch's supplier payments (PRD-001 FR-BNK-12, FR-AP-13; rev 0.154; p1-payments-out
-- task 2.7, design D8).
--
-- When bank import matches a supplier payment a batch released to its statement line, the worker renders the
-- organisation's remittance template to PDF, keeps it as the payment's evidence, and emails it to the supplier's
-- remittance address: an email of purpose remittance, recorded as a document send of the payment. A posted bill payment
-- joins the documents that are sent (FY196); nothing else changes. The widened check is validated by 000188.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.email_message DROP CONSTRAINT email_message_purpose_known;
ALTER TABLE fyniq.email_message ADD CONSTRAINT email_message_purpose_known
  CHECK (purpose IN ('test', 'sign_in_alert', 'notification', 'digest', 'invoice', 'quote', 'reminder', 'statement', 'remittance')) NOT VALID;

-- +goose StatementBegin
-- document_send_guard as in 000139, and: a posted bill payment's remittance advice is sent too (FR-BNK-12).
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
  IF doc.type NOT IN ('quote', 'sales_order', 'invoice', 'statement', 'bill_payment') OR doc.status <> 'posted'
     OR doc.current_version <> NEW.document_version THEN
    RAISE EXCEPTION 'Only the current version of a sent quote or statement, an issued invoice or a posted payment is sent, not version % of a % that is %',
      NEW.document_version, doc.type, doc.status
      USING ERRCODE = 'FY196', HINT = 'Issue or send it first; a draft is never sent (FR-AR-06, FR-BNK-12).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
