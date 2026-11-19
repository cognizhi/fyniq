-- A captured file that shows an NRIC or FIN in full is flagged, and never sent to a model (PRD-001 FR-DOC-09, AI-02, AI-20; p1-document-capture).
--
-- The upload's own check says whether a file's text shows an identity number in full. The capture keeps that flag with the file, so the
-- reading refuses it and the draft's evidence carries it, as a claim's receipt does. What the file is, flag included, never changes (FY950).
-- A document being read can also be dismissed by a person, so a reading that never ended (a crash mid-call) does not strand it.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document_capture ADD COLUMN nric_flagged boolean NOT NULL DEFAULT false;
GRANT INSERT (nric_flagged) ON fyniq.document_capture TO fyniq_api;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.document_capture_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A captured document is kept: it is dismissed, never deleted' USING ERRCODE = 'FY950',
      HINT = 'Dismiss it from the document inbox (FR-DOC-14).';
  END IF;
  IF (NEW.source, NEW.file_name, NEW.media_type, NEW.size_bytes, NEW.sha256, NEW.blob_key, NEW.message_id, NEW.inbound_file_id, NEW.owner_user_id,
      NEW.captured_by, NEW.created_at, NEW.nric_flagged)
     IS DISTINCT FROM (OLD.source, OLD.file_name, OLD.media_type, OLD.size_bytes, OLD.sha256, OLD.blob_key, OLD.message_id, OLD.inbound_file_id,
      OLD.owner_user_id, OLD.captured_by, OLD.created_at, OLD.nric_flagged) THEN
    RAISE EXCEPTION 'What a captured document is never changes' USING ERRCODE = 'FY950',
      HINT = 'Capture the file again (FR-DOC-14).';
  END IF;
  IF NEW.status <> OLD.status AND NOT (
       (OLD.status IN ('received', 'failed', 'read') AND NEW.status = 'reading')
    OR (OLD.status = 'reading' AND NEW.status IN ('read', 'failed', 'dismissed'))
    OR (OLD.status IN ('received', 'read', 'failed', 'duplicate') AND NEW.status = 'dismissed')
    OR (OLD.status = 'read' AND NEW.status = 'drafted')) THEN
    RAISE EXCEPTION 'A captured document moves from % to % along the lifecycle only', OLD.status, NEW.status USING ERRCODE = 'FY950',
      HINT = 'received, then reading, read and drafted; a failed one is read again; a dismissed or drafted one is final (FR-DOC-14).';
  END IF;
  IF OLD.status IN ('drafted', 'dismissed') THEN
    RAISE EXCEPTION 'A drafted or dismissed captured document is final' USING ERRCODE = 'FY950';
  END IF;
  IF NEW.draft_id IS NOT NULL AND (OLD.draft_id IS NOT NULL OR NEW.status <> 'drafted') THEN
    RAISE EXCEPTION 'A captured document is drafted once, from its reading' USING ERRCODE = 'FY950';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
