-- Bank lines matched to receipts and bill payments (PRD-001 FR-BNK-07, FR-BNK-08, FR-AR-18; p1-bank-import-reconcile
-- task 4.3, design D5).
--
-- Accepting a match to invoices records a receipt from the line, and to bills a bill payment, through their own
-- commands; a receipt, payment, deposit or refund recorded by hand is matched as it is, posting nothing new. So that
-- the books record exactly what the bank said, whatever the path:
--   - bank_match_posting now checks every match but a transfer's (FY228): a receipt or bill payment made from the line
--     names its own posted document and that document's entry, dated on the line's booking date; a document recorded
--     by hand is a posted receipt, payment, deposit or refund; and either posts the line's amount, in its currency, on
--     the line's money account, in an entry of its current version that nothing has reversed;
--   - a document with a live match is never reversed (FY229): its match is undone first, in the same transaction, so a
--     void never leaves a line matched to a reversed document.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.bank_match_posting() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  l     fyniq.bank_line;
  kinds text[];
BEGIN
  SELECT * INTO l FROM fyniq.bank_line WHERE organisation_id = NEW.organisation_id AND id = NEW.line_id;
  kinds := CASE NEW.kind
    WHEN 'categorised' THEN ARRAY['bank_transaction']
    WHEN 'suspense' THEN ARRAY['bank_transaction']
    WHEN 'receipt' THEN ARRAY['customer_receipt']
    WHEN 'bill_payment' THEN ARRAY['bill_payment']
    ELSE ARRAY['customer_receipt', 'bill_payment', 'customer_deposit', 'customer_refund', 'supplier_refund'] END;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.document d
    WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id AND d.type = ANY (kinds) AND d.status = 'posted'
      AND (d.type <> 'bank_transaction' OR EXISTS (
        SELECT 1 FROM fyniq.document_version v WHERE v.organisation_id = d.organisation_id AND v.document_id = d.id
          AND v.version = d.current_version AND v.payload->>'bank_line_id' = NEW.line_id::text))
      AND EXISTS (
        SELECT 1 FROM fyniq.journal_entry e
        JOIN fyniq.money_account m ON m.organisation_id = e.organisation_id AND m.id = l.money_account_id
        JOIN fyniq.journal_line jl ON jl.organisation_id = e.organisation_id AND jl.entry_id = e.id AND jl.account_id = m.account_id
        WHERE e.organisation_id = NEW.organisation_id AND e.source_id = d.id::text AND e.source_version = d.current_version
          AND e.reverses_entry_id IS NULL AND jl.currency = l.currency AND jl.amount = l.amount
          AND (NEW.entry_id IS NULL OR (e.id = NEW.entry_id AND e.posting_date = l.booking_date))
          AND NOT EXISTS (SELECT 1 FROM fyniq.journal_entry r WHERE r.organisation_id = e.organisation_id AND r.reverses_entry_id = e.id))
  ) THEN
    RAISE EXCEPTION 'The % match of bank line % names no document of that kind, or one that does not post the line''s amount through its account', NEW.kind, NEW.line_id
      USING ERRCODE = 'FY228', HINT = 'Match the line through Banking, which records its document with the line''s amount and date (FR-BNK-07).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
DROP TRIGGER bank_match_posting ON fyniq.bank_match;
CREATE TRIGGER bank_match_posting BEFORE INSERT ON fyniq.bank_match
  FOR EACH ROW WHEN (NEW.kind <> 'transfer') EXECUTE FUNCTION fyniq.bank_match_posting();

-- +goose StatementBegin
CREATE FUNCTION fyniq.document_reversed_unmatched() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM fyniq.bank_match b WHERE b.organisation_id = NEW.organisation_id AND b.document_id = NEW.id AND b.undone_at IS NULL) THEN
    RAISE EXCEPTION 'Document % is matched to a bank line', NEW.id
      USING ERRCODE = 'FY229', HINT = 'Undo the line''s match in Banking first: that reverses what it made from the line (FR-BNK-07).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER document_reversed_unmatched BEFORE UPDATE OF status ON fyniq.document
  FOR EACH ROW WHEN (NEW.status = 'reversed' AND OLD.status IS DISTINCT FROM 'reversed') EXECUTE FUNCTION fyniq.document_reversed_unmatched();

REVOKE ALL ON FUNCTION fyniq.document_reversed_unmatched() FROM PUBLIC;
