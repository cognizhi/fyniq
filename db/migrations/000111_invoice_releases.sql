-- Releases of an invoice's spread revenue (PRD-001 FR-GL-03, ENG-05; rev 0.118; p1-sales task 1.5).
--
-- An invoice line spread over months credits 2400 Deferred revenue, and the platform job invoices.releases posts each
-- month's part, Dr 2400 and Cr the line's account (invoice_release@1), as the principal "scheduler:invoices.releases"
-- with the worker's role, which has held INSERT on journal_entry and journal_line since 000004. As a bill's release
-- (000089), a release names the invoice version whose spread it releases as its source, so:
--   - journal_entry_invoice_release refuses a release (not its reversal) that does not name a posted invoice at its
--     current version (FY172): a voided invoice, or a version an edit replaced, releases nothing more;
--   - document_edit_posted and document_reversed_posted (FY055, FY056) no longer count an invoice's release as its
--     entry either, so an edit or a void still has to post and reverse the invoice's own.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.journal_entry_invoice_release() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.document d
    WHERE d.organisation_id = NEW.organisation_id AND d.id::text = NEW.source_id AND d.type = 'invoice'
      AND d.status = 'posted' AND d.current_version = NEW.source_version
  ) THEN
    RAISE EXCEPTION 'Release % names no issued invoice at its current version', NEW.posting_identity
      USING ERRCODE = 'FY172', HINT = 'A release releases the spread of an issued invoice''s current version (FR-GL-03).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER journal_entry_invoice_release BEFORE INSERT ON fyniq.journal_entry
  FOR EACH ROW WHEN (NEW.source_type = 'invoice_release' AND NEW.reverses_entry_id IS NULL)
  EXECUTE FUNCTION fyniq.journal_entry_invoice_release();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.document_edit_posted() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.journal_entry e
    WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.id::text
      AND e.source_type NOT IN ('bill_release', 'invoice_release')
      AND e.source_version = NEW.current_version AND e.reverses_entry_id IS NULL
  ) THEN
    RAISE EXCEPTION 'The edit of document % to version % posts that version', NEW.id, NEW.current_version
      USING ERRCODE = 'FY055', HINT = 'An edit of a posted document posts its new version in the same transaction (FR-GL-12).';
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.journal_entry e
    JOIN fyniq.journal_entry r ON r.organisation_id = e.organisation_id AND r.reverses_entry_id = e.id
    WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.id::text
      AND e.source_type NOT IN ('bill_release', 'invoice_release')
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
  IF NEW.type = 'purchase_order' THEN
    RETURN NULL; -- a purchase order posts nothing, so closing it reverses nothing
  END IF;
  IF NEW.type = 'credit_application' AND NOT EXISTS (
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
      AND e.source_type NOT IN ('bill_release', 'invoice_release')
      AND e.source_version = NEW.current_version AND e.reverses_entry_id IS NULL
  ) THEN
    RAISE EXCEPTION 'Document % is reversed only with the reversal of its entry', NEW.id
      USING ERRCODE = 'FY056', HINT = 'Reverse it through the application, which posts the reversing entry in the same transaction (FR-GL-12).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
