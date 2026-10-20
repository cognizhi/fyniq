-- Edits of posted documents (PRD-001 FR-GL-12, posting rule "Edits of posted documents"; rev 0.54;
-- p1-general-ledger-close task 2.3).
--
-- A posted document is edited by saving its next version and, in the same transaction, reversing the entry of the
-- version it had and posting the entry of the new one; the document stays posted, and records who posted the edit and
-- when. document_guard allows exactly that: a posted document's version moves forward only with who posted it set anew
-- (FY053), and a reversed document never changes version (FY055). document_edit_posted checks at commit that the
-- version a posted document moved to has its entry, and that the entry of the version it moved from is reversed
-- (FY055), so no path edits a posted document without the postings. Journals also join the approval engine's edit
-- operation, `journal.edit`: in Founder mode an edit is high-risk, like a void (FM-1). The widened check is added NOT
-- VALID and validated by 000062 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- document_guard as in 000052, and: a posted document's version moves forward only as an edit, which names who posted
-- it and when anew; a reversed document's version never moves.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.document_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  edit boolean := OLD.status = 'posted' AND NEW.status = 'posted' AND NEW.current_version > OLD.current_version;
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
  IF OLD.status = 'reversed' AND NEW.current_version <> OLD.current_version THEN
    RAISE EXCEPTION 'Document % is reversed, so it never changes', OLD.id USING ERRCODE = 'FY055';
  END IF;
  IF (OLD.status = 'draft' AND NEW.status = 'posted') OR edit THEN
    IF NEW.posted_by IS NULL OR NEW.posted_at IS NULL OR (edit AND NEW.posted_at IS NOT DISTINCT FROM OLD.posted_at) THEN
      RAISE EXCEPTION 'Posting document % names who posted it and when', OLD.id USING ERRCODE = 'FY053',
        HINT = 'Post it, or post its edit, through the application, which records the actor.';
    END IF;
  ELSIF (NEW.posted_by, NEW.posted_at) IS DISTINCT FROM (OLD.posted_by, OLD.posted_at) THEN
    RAISE EXCEPTION 'Who posted document % is set when it posts or its edit posts, and never changes otherwise', OLD.id
      USING ERRCODE = 'FY053';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- At commit: the version a posted document moved to has its entry, and the entry of the version it moved from is
-- reversed, so an edit posts the reversal and the replacement together (FR-GL-12).
-- +goose StatementBegin
CREATE FUNCTION fyniq.document_edit_posted() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.journal_entry e
    WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.id::text
      AND e.source_version = NEW.current_version AND e.reverses_entry_id IS NULL
  ) THEN
    RAISE EXCEPTION 'The edit of document % to version % posts that version', NEW.id, NEW.current_version
      USING ERRCODE = 'FY055', HINT = 'An edit of a posted document posts its new version in the same transaction (FR-GL-12).';
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.journal_entry e
    JOIN fyniq.journal_entry r ON r.organisation_id = e.organisation_id AND r.reverses_entry_id = e.id
    WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.id::text
      AND e.source_version = OLD.current_version AND e.reverses_entry_id IS NULL
  ) THEN
    RAISE EXCEPTION 'The edit of document % reverses the entry of version %', NEW.id, OLD.current_version
      USING ERRCODE = 'FY055', HINT = 'An edit of a posted document reverses the entry it replaces in the same transaction (FR-GL-12).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER document_edit_posted AFTER UPDATE OF current_version ON fyniq.document
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW WHEN (OLD.status = 'posted' AND NEW.current_version <> OLD.current_version)
  EXECUTE FUNCTION fyniq.document_edit_posted();

ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_object_known CHECK (
  (object_type, operation) IN (('organisation_setting', 'organisation_setting.change'), ('membership', 'role.assign'),
                               ('membership', 'role.revoke'), ('member_invitation', 'member_invitation.create'),
                               ('approval_policy', 'approval_policy.change'), ('statutory_pack', 'statutory_pack.activate'),
                               ('team_mode', 'team_mode.change'), ('period', 'period.reopen'),
                               ('journal', 'journal.post'), ('journal', 'journal.reverse'), ('journal', 'journal.edit'))) NOT VALID;
