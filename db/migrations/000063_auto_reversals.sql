-- Automatic reversals (PRD-001 FR-GL-02, FR-AUT-01, FR-AUT-02; rev 0.55; p1-general-ledger-close tasks 2.4, 5.1).
--
-- A journal may carry the date the scheduler reverses it by itself. The scheduler runs as a platform job under the
-- worker's role and its own principal ("scheduler:journals.auto-reverse"), so the worker may now move a document's
-- status (UPDATE (status) only; it saves no version and posts nothing but the reversal). Because a second role can now
-- mark documents reversed, the database checks what the application always did: document_reversed_posted, deferred to
-- commit, refuses a move from posted to reversed unless the entry of the document's current version is reversed in
-- the ledger (FY056), whichever role makes it.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.document_reversed_posted() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.journal_entry e
    JOIN fyniq.journal_entry r ON r.organisation_id = e.organisation_id AND r.reverses_entry_id = e.id
    WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.id::text
      AND e.source_version = NEW.current_version AND e.reverses_entry_id IS NULL
  ) THEN
    RAISE EXCEPTION 'Document % is reversed only with the reversal of its entry', NEW.id
      USING ERRCODE = 'FY056', HINT = 'Reverse it through the application, which posts the reversing entry in the same transaction (FR-GL-12).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER document_reversed_posted AFTER UPDATE OF status ON fyniq.document
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW WHEN (OLD.status = 'posted' AND NEW.status = 'reversed')
  EXECUTE FUNCTION fyniq.document_reversed_posted();

GRANT UPDATE (status) ON fyniq.document TO fyniq_worker;
