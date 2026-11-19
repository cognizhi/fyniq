-- The document inbox: every captured file, its reading and the draft a person made of it (PRD-001 FR-DOC-12, FR-DOC-14, FR-AP-04, FR-AP-12;
-- p1-document-capture design D1).
--
-- A file enters by upload, paste or an accepted email-in message. It is kept under its own blob key in the staging store (never as evidence
-- until a person makes a draft of it), with its hash. The model's reading of it (the fields and their confidences, as the agent
-- document_capture answered) and the platform's checks of that reading are kept beside it as data labelled so, apart from the file: they
-- never decide a figure. A person who may enter bills or claims makes the draft; the draft's record is linked here. A row is never deleted:
-- a dismissal is a status, and the file's retention is the evidence rules'.
--
-- The status moves along the lifecycle only (FY950); what the file is (its name, type, size, hash, blob key, source, message and who captured
-- it) never changes.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.document_capture (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  source          text        NOT NULL,  -- upload, paste or email
  file_name       text        NOT NULL,  -- as sent: data, never a path
  media_type      text        NOT NULL,  -- decided from the content
  size_bytes      bigint      NOT NULL,
  sha256          text        NOT NULL,  -- of the file as stored
  blob_key        text        NOT NULL,
  message_id      uuid,                  -- the accepted email message it came from
  inbound_file_id uuid,                  -- and its file: each is adopted once
  owner_user_id   uuid,                  -- a member's own email-in address's member: only they see it; none: the organisation's
  captured_by     text        NOT NULL,  -- the actor of the audit trail
  status          text        NOT NULL DEFAULT 'received',
  kind            text,                  -- what the reading says it is
  reading         jsonb,                 -- the model's reading: untrusted data, never a figure
  reading_model   text,
  reading_prompt  text,
  readings        integer     NOT NULL DEFAULT 0,
  read_at         timestamptz,
  checks          jsonb,                 -- the platform's checks of the reading
  duplicate_of    uuid,
  draft_kind      text,                  -- bill or claim
  draft_id        uuid,
  drafted_by      uuid,
  drafted_at      timestamptz,
  dismissed_by    uuid,
  dismissed_at    timestamptz,
  dismiss_reason  text,
  last_error      text,
  created_at      timestamptz NOT NULL,
  version         integer     NOT NULL DEFAULT 1,
  CONSTRAINT document_capture_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT document_capture_source_known CHECK (source IN ('upload', 'paste', 'email')),
  CONSTRAINT document_capture_status_known CHECK (status IN ('received', 'reading', 'read', 'duplicate', 'drafted', 'dismissed', 'failed')),
  CONSTRAINT document_capture_kind_known CHECK (kind IN ('bill', 'receipt', 'sales_invoice', 'statement', 'contract', 'other')),
  CONSTRAINT document_capture_draft_known CHECK ((draft_kind IS NULL AND draft_id IS NULL AND drafted_by IS NULL AND drafted_at IS NULL)
    OR (draft_kind IN ('bill', 'claim') AND draft_id IS NOT NULL AND drafted_by IS NOT NULL AND drafted_at IS NOT NULL)),
  CONSTRAINT document_capture_dismissed_pair CHECK ((dismissed_by IS NULL) = (dismissed_at IS NULL)),
  CONSTRAINT document_capture_email_pair CHECK ((source = 'email') = (message_id IS NOT NULL AND inbound_file_id IS NOT NULL)),
  CONSTRAINT document_capture_lengths CHECK (length(file_name) BETWEEN 1 AND 255 AND length(media_type) BETWEEN 3 AND 100
    AND size_bytes BETWEEN 1 AND 26214400 AND sha256 ~ '^[0-9a-f]{64}$' AND length(captured_by) BETWEEN 3 AND 120
    AND blob_key ~ '^capture/[0-9a-f-]{36}/[0-9a-f-]{36}$' AND length(dismiss_reason) <= 500 AND length(last_error) <= 500
    AND length(reading_model) <= 200 AND length(reading_prompt) <= 60 AND readings BETWEEN 0 AND 20),
  CONSTRAINT document_capture_reading_size CHECK (pg_column_size(reading) <= 262144 AND pg_column_size(checks) <= 65536),
  CONSTRAINT document_capture_duplicate_fkey FOREIGN KEY (organisation_id, duplicate_of) REFERENCES fyniq.document_capture (organisation_id, id)
);
CREATE UNIQUE INDEX document_capture_inbound_once ON fyniq.document_capture (organisation_id, inbound_file_id) WHERE inbound_file_id IS NOT NULL;
CREATE INDEX document_capture_status ON fyniq.document_capture (organisation_id, status, created_at DESC);
CREATE INDEX document_capture_hash ON fyniq.document_capture (organisation_id, sha256);

-- +goose StatementBegin
CREATE FUNCTION fyniq.document_capture_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A captured document is kept: it is dismissed, never deleted' USING ERRCODE = 'FY950',
      HINT = 'Dismiss it from the document inbox (FR-DOC-14).';
  END IF;
  IF (NEW.source, NEW.file_name, NEW.media_type, NEW.size_bytes, NEW.sha256, NEW.blob_key, NEW.message_id, NEW.inbound_file_id, NEW.owner_user_id,
      NEW.captured_by, NEW.created_at)
     IS DISTINCT FROM (OLD.source, OLD.file_name, OLD.media_type, OLD.size_bytes, OLD.sha256, OLD.blob_key, OLD.message_id, OLD.inbound_file_id,
      OLD.owner_user_id, OLD.captured_by, OLD.created_at) THEN
    RAISE EXCEPTION 'What a captured document is never changes' USING ERRCODE = 'FY950',
      HINT = 'Capture the file again (FR-DOC-14).';
  END IF;
  IF NEW.status <> OLD.status AND NOT (
       (OLD.status IN ('received', 'failed', 'read') AND NEW.status = 'reading')
    OR (OLD.status = 'reading' AND NEW.status IN ('read', 'failed'))
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
CREATE TRIGGER document_capture_guard BEFORE UPDATE OR DELETE ON fyniq.document_capture
  FOR EACH ROW EXECUTE FUNCTION fyniq.document_capture_guard();
CREATE TRIGGER document_capture_no_truncate BEFORE TRUNCATE ON fyniq.document_capture
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.document_capture ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.document_capture FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.document_capture
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE OF status ON fyniq.document_capture
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document_capture', 'id');

-- The API alone writes it, as the person who captured, read or drafted; nothing else holds a grant, and the AI's role none (CMP-AI-01): the
-- reading is made by the API's own call of the model and stored as data.
REVOKE ALL ON fyniq.document_capture FROM PUBLIC;
GRANT SELECT ON fyniq.document_capture TO fyniq_api, fyniq_worker, fyniq_readonly; -- the worker reads it for the tenant export
GRANT INSERT (source, file_name, media_type, size_bytes, sha256, blob_key, message_id, inbound_file_id, owner_user_id, captured_by, status,
  duplicate_of, created_at) ON fyniq.document_capture TO fyniq_api;
GRANT UPDATE (status, kind, reading, reading_model, reading_prompt, readings, read_at, checks, draft_kind, draft_id, drafted_by, drafted_at,
  dismissed_by, dismissed_at, dismiss_reason, last_error, duplicate_of, version) ON fyniq.document_capture TO fyniq_api;
