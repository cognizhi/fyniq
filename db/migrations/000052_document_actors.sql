-- Who saved each version of a document, and who posted it and when (PRD-001 FR-GL-01: the preparer and the approver
-- are recorded; SOD-3 compares them; rev 0.48; p1-general-ledger-close task 2.1).
--
-- A version records the actor who saved it, and a document the actor who posted it, in the transaction that does it,
-- as its audit event records them ("user:<id>", "ai:<client>", "service:<name>"). Versions never change (FY002), so
-- neither does who saved one. Who posted a document is set once, with its move from draft to posted, and never changes
-- after (FY053). Rows from before this migration have neither: the audit trail keeps who acted on them.
--
-- The format checks are added NOT VALID and validated by 000053 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document_version ADD COLUMN created_by text;
ALTER TABLE fyniq.document ADD COLUMN posted_by text;
ALTER TABLE fyniq.document ADD COLUMN posted_at timestamptz;

ALTER TABLE fyniq.document_version ADD CONSTRAINT document_version_created_by_format
  CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$') NOT VALID;
ALTER TABLE fyniq.document ADD CONSTRAINT document_posted_by_format
  CHECK (posted_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$') NOT VALID;
ALTER TABLE fyniq.document ADD CONSTRAINT document_posted_together
  CHECK ((posted_by IS NULL) = (posted_at IS NULL)) NOT VALID;

-- A new version names who saved it.
-- +goose StatementBegin
CREATE FUNCTION fyniq.document_version_created_by() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.created_by IS NULL THEN
    RAISE EXCEPTION 'A version of document % names who saved it', NEW.document_id USING ERRCODE = 'FY053',
      HINT = 'Save it through the application, which records the actor.';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER document_version_created_by BEFORE INSERT ON fyniq.document_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.document_version_created_by();

-- document_guard as in 000005, and: who posted a document is set with its move from draft to posted, and only then.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.document_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.type IS DISTINCT FROM OLD.type OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION 'The type of document % cannot change', OLD.id USING ERRCODE = 'FY050';
  END IF;
  IF NEW.status IS DISTINCT FROM OLD.status AND NOT (
       (OLD.status = 'draft' AND NEW.status = 'posted') OR (OLD.status = 'posted' AND NEW.status = 'reversed')) THEN
    RAISE EXCEPTION 'Document % cannot go from % to %', OLD.id, OLD.status, NEW.status
      USING ERRCODE = 'FY051',
            HINT = 'A draft is posted, a posted document is reversed, and a reversed one stays reversed (FR-GL-12).';
  END IF;
  IF NEW.current_version < OLD.current_version THEN
    RAISE EXCEPTION 'Document % cannot go back to version %', OLD.id, NEW.current_version USING ERRCODE = 'FY052';
  END IF;
  IF OLD.status = 'draft' AND NEW.status = 'posted' THEN
    IF NEW.posted_by IS NULL OR NEW.posted_at IS NULL THEN
      RAISE EXCEPTION 'Posting document % names who posted it and when', OLD.id USING ERRCODE = 'FY053',
        HINT = 'Post it through the application, which records the actor.';
    END IF;
  ELSIF (NEW.posted_by, NEW.posted_at) IS DISTINCT FROM (OLD.posted_by, OLD.posted_at) THEN
    RAISE EXCEPTION 'Who posted document % is set when it posts, and never changes', OLD.id USING ERRCODE = 'FY053';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT INSERT (created_by) ON fyniq.document_version TO fyniq_api, fyniq_executor;
GRANT UPDATE (posted_by, posted_at) ON fyniq.document TO fyniq_api, fyniq_executor;
