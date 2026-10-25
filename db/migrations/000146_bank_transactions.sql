-- Categorised bank lines (PRD-001 FR-BNK-06, FR-BNK-11, Appendix B.1; p1-bank-import-reconcile task 4.1, design D4).
--
-- A bank line categorised by a person or a rule becomes a document of its own, bank_transaction, unnumbered, posted at
-- once by the rule bank_transaction@1: Dr or Cr the money account with the line's signed gross, its GST to 2200, the
-- rest to each part's account, or to 1210 or 2400 for a part spread over months. Its version keeps the GST and the
-- exchange rate it posted with. Undoing it reverses its entry and the document; a new categorisation is a new document.
-- A spread part releases monthly from the posting month (bank_out_release@1 from 1210, bank_in_release@1 from 2400) by
-- the platform job bank.releases, as the principal "scheduler:bank.releases" with the worker's role, so:
--   - journal_entry_bank_release refuses a release (not its reversal) that does not name a posted bank transaction at
--     its current version (FY227);
--   - document_edit_posted and document_reversed_posted (FY055, FY056) no longer count a bank release as the
--     transaction's own entry, so an undo still has to reverse it;
--   - a categorised or suspense match names the bank transaction its own line posted and that transaction's entry,
--     whose line on the money account is the line's amount on its booking date (FY228): the books record exactly what
--     the bank said.
-- The widened checks are added NOT VALID and validated by 000147 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation', 'bill', 'bill_payment', 'vendor_credit', 'credit_application',
                  'supplier_refund', 'purchase_order', 'invoice', 'customer_receipt', 'write_off', 'credit_note',
                  'customer_credit_application', 'customer_refund', 'customer_deposit', 'quote', 'sales_order', 'statement',
                  'bank_transaction')) NOT VALID;

-- A categorised line and one in suspense post a bank transaction: the match names it and its entry.
ALTER TABLE fyniq.bank_match DROP CONSTRAINT bank_match_kind_refs;
ALTER TABLE fyniq.bank_match ADD CONSTRAINT bank_match_kind_refs CHECK (CASE kind
    WHEN 'categorised' THEN entry_id IS NOT NULL AND document_id IS NOT NULL AND transfer_id IS NULL
    WHEN 'suspense' THEN entry_id IS NOT NULL AND document_id IS NOT NULL AND transfer_id IS NULL
    WHEN 'receipt' THEN document_id IS NOT NULL AND transfer_id IS NULL
    WHEN 'bill_payment' THEN document_id IS NOT NULL AND transfer_id IS NULL
    WHEN 'recorded' THEN document_id IS NOT NULL AND entry_id IS NULL AND transfer_id IS NULL
    ELSE transfer_id IS NOT NULL AND document_id IS NULL END) NOT VALID;

-- +goose StatementBegin
CREATE FUNCTION fyniq.journal_entry_bank_release() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.document d
    WHERE d.organisation_id = NEW.organisation_id AND d.id::text = NEW.source_id AND d.type = 'bank_transaction'
      AND d.status = 'posted' AND d.current_version = NEW.source_version
  ) THEN
    RAISE EXCEPTION 'Release % names no posted bank transaction at its current version', NEW.posting_identity
      USING ERRCODE = 'FY227', HINT = 'A release releases the spread of a categorised bank line that is still categorised (FR-GL-03, Appendix B.1).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER journal_entry_bank_release BEFORE INSERT ON fyniq.journal_entry
  FOR EACH ROW WHEN (NEW.source_type IN ('bank_out_release', 'bank_in_release') AND NEW.reverses_entry_id IS NULL)
  EXECUTE FUNCTION fyniq.journal_entry_bank_release();

-- +goose StatementBegin
CREATE FUNCTION fyniq.bank_match_posting() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  l fyniq.bank_line;
BEGIN
  SELECT * INTO l FROM fyniq.bank_line WHERE organisation_id = NEW.organisation_id AND id = NEW.line_id;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.document d
    WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id AND d.type = 'bank_transaction' AND d.status = 'posted'
      AND EXISTS (SELECT 1 FROM fyniq.document_version v WHERE v.organisation_id = d.organisation_id AND v.document_id = d.id
                    AND v.version = d.current_version AND v.payload->>'bank_line_id' = NEW.line_id::text)
  ) OR NOT EXISTS (
    SELECT 1 FROM fyniq.journal_entry e
    JOIN fyniq.money_account m ON m.organisation_id = e.organisation_id AND m.id = l.money_account_id
    JOIN fyniq.journal_line jl ON jl.organisation_id = e.organisation_id AND jl.entry_id = e.id AND jl.account_id = m.account_id
    WHERE e.organisation_id = NEW.organisation_id AND e.id = NEW.entry_id AND e.source_type = 'bank_transaction'
      AND e.source_id = NEW.document_id::text AND e.reverses_entry_id IS NULL AND e.posting_date = l.booking_date
      AND jl.currency = l.currency AND jl.amount = l.amount
  ) THEN
    RAISE EXCEPTION 'The % match of bank line % names no bank transaction of that line, or an entry that does not post its amount', NEW.kind, NEW.line_id
      USING ERRCODE = 'FY228', HINT = 'Categorise the line through Banking, which posts its bank transaction with the line''s amount and date (FR-BNK-06).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bank_match_posting BEFORE INSERT ON fyniq.bank_match
  FOR EACH ROW WHEN (NEW.kind IN ('categorised', 'suspense')) EXECUTE FUNCTION fyniq.bank_match_posting();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.document_edit_posted() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.journal_entry e
    WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.id::text
      AND e.source_type NOT IN ('bill_release', 'invoice_release', 'bank_out_release', 'bank_in_release')
      AND e.source_version = NEW.current_version AND e.reverses_entry_id IS NULL
  ) THEN
    RAISE EXCEPTION 'The edit of document % to version % posts that version', NEW.id, NEW.current_version
      USING ERRCODE = 'FY055', HINT = 'An edit of a posted document posts its new version in the same transaction (FR-GL-12).';
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.journal_entry e
    JOIN fyniq.journal_entry r ON r.organisation_id = e.organisation_id AND r.reverses_entry_id = e.id
    WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.id::text
      AND e.source_type NOT IN ('bill_release', 'invoice_release', 'bank_out_release', 'bank_in_release')
      AND e.source_version = OLD.current_version AND e.reverses_entry_id IS NULL
  ) THEN
    RAISE EXCEPTION 'The edit of document % reverses the entry of version %', NEW.id, OLD.current_version
      USING ERRCODE = 'FY055', HINT = 'An edit of a posted document reverses the entry it replaces in the same transaction (FR-GL-12).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.document_reversed_posted() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.type IN ('purchase_order', 'quote', 'sales_order') THEN
    RETURN NULL; -- a purchase order, a quote or a sales order posts nothing, so closing or cancelling it reverses nothing
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

REVOKE ALL ON FUNCTION fyniq.journal_entry_bank_release(), fyniq.bank_match_posting() FROM PUBLIC;
