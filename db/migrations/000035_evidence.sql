-- Evidence on records (PRD-001 FR-DOC-01, 02, 05, 07, 08, 13; p1-documents-evidence tasks 2.1 and 2.2).
--
-- A file attached to a source document (a manual journal today; invoices, bills and the other documents as
-- they arrive) with its evidence type, a short note, the date it relates to, its source, its data class and
-- the SHA-256 of the file as received and as stored. Two phases (FR-DOC-13): an upload lands in the staging
-- store and is attached while its parent is a draft; once the parent is posted, the worker copies it into
-- the retention-locked store and records the key and retain-until here ('final'). Evidence is never
-- deleted: while the parent is a draft a file is removed by marking it so; once the parent is not a draft,
-- evidence is append-only. The database enforces all of it, whoever asks (FR-DOC-07, CTL-AUD-06). Its
-- retain-until here is authoritative, and object metadata never is (FR-DOC-02).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.evidence (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  document_id     uuid        NOT NULL,
  evidence_type   text        NOT NULL,
  note            text,
  relates_on      date,
  file_name       text        NOT NULL,
  media_type      text        NOT NULL,
  size_bytes      bigint      NOT NULL,
  sha256          text        NOT NULL,
  original_sha256 text        NOT NULL,
  source          text        NOT NULL,
  data_class      text        NOT NULL DEFAULT 'general',
  staging_key     text        NOT NULL,
  uploaded_by     text        NOT NULL,
  uploaded_at     timestamptz NOT NULL DEFAULT now(),
  state           text        NOT NULL DEFAULT 'attached',
  removed_at      timestamptz,
  locked_key      text,
  retain_until    timestamptz,
  finalised_at    timestamptz,
  version         integer     NOT NULL DEFAULT 1,
  CONSTRAINT evidence_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT evidence_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT evidence_document_fkey FOREIGN KEY (organisation_id, document_id)
    REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT evidence_staging_key UNIQUE (organisation_id, staging_key),
  CONSTRAINT evidence_lengths CHECK (length(btrim(evidence_type)) BETWEEN 1 AND 100 AND length(note) BETWEEN 1 AND 1000
    AND length(file_name) BETWEEN 1 AND 255 AND length(uploaded_by) BETWEEN 1 AND 200),
  CONSTRAINT evidence_media_type_known CHECK (media_type IN ('application/pdf', 'image/png', 'image/jpeg', 'image/webp',
    'text/plain', 'text/csv', 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    'application/vnd.openxmlformats-officedocument.wordprocessingml.document', 'message/rfc822', 'application/vnd.ms-outlook')),
  CONSTRAINT evidence_size_range CHECK (size_bytes BETWEEN 1 AND 26214400),
  CONSTRAINT evidence_hash_format CHECK (sha256 ~ '^[0-9a-f]{64}$' AND original_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT evidence_source_known CHECK (source IN ('upload', 'paste', 'email', 'api')),
  CONSTRAINT evidence_data_class_known CHECK (data_class IN ('general', 'personal', 'bank', 'payroll', 'identity')),
  CONSTRAINT evidence_key_format CHECK (staging_key ~ '^staging/[0-9a-f-]{36}/[0-9a-f-]{36}$'
    AND locked_key ~ '^evidence/[0-9a-f-]{36}/[0-9a-f-]{36}$'),
  CONSTRAINT evidence_state_known CHECK (state IN ('attached', 'removed', 'final')),
  CONSTRAINT evidence_state_fields CHECK (
    (state = 'attached' AND removed_at IS NULL AND locked_key IS NULL AND retain_until IS NULL AND finalised_at IS NULL)
    OR (state = 'removed' AND removed_at IS NOT NULL AND locked_key IS NULL AND retain_until IS NULL AND finalised_at IS NULL)
    OR (state = 'final' AND removed_at IS NULL AND locked_key IS NOT NULL AND retain_until IS NOT NULL AND finalised_at IS NOT NULL)),
  CONSTRAINT evidence_version_positive CHECK (version > 0)
);
CREATE INDEX evidence_document ON fyniq.evidence (organisation_id, document_id, id);

-- Evidence never loses what it is: its file, hash, parent, uploader and source stay as attached. Its state
-- moves attached → removed only while its parent is a draft, and attached → final only once the parent is
-- not a draft; a final file is append-only, and its retain-until only moves later (FR-DOC-02, FR-DOC-07).
-- For a removal the parent's row is read FOR SHARE, so a posting waits for the removal to commit, and the
-- other way round.
-- +goose StatementBegin
CREATE FUNCTION fyniq.evidence_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  parent text;
BEGIN
  IF (NEW.organisation_id, NEW.id, NEW.document_id, NEW.evidence_type, NEW.note, NEW.relates_on, NEW.file_name, NEW.media_type,
      NEW.size_bytes, NEW.sha256, NEW.original_sha256, NEW.source, NEW.data_class, NEW.staging_key, NEW.uploaded_by, NEW.uploaded_at)
     IS DISTINCT FROM
     (OLD.organisation_id, OLD.id, OLD.document_id, OLD.evidence_type, OLD.note, OLD.relates_on, OLD.file_name, OLD.media_type,
      OLD.size_bytes, OLD.sha256, OLD.original_sha256, OLD.source, OLD.data_class, OLD.staging_key, OLD.uploaded_by, OLD.uploaded_at) THEN
    RAISE EXCEPTION 'Evidence % cannot change what it is: its file, hash, parent, uploader and details stay as attached', OLD.id
      USING ERRCODE = 'FY120', HINT = 'Attach a new file instead; once the record is final, supersede it with a reason (FR-DOC-07).';
  END IF;
  -- A document never goes back to draft, so only a removal needs the parent held until commit.
  SELECT d.status INTO parent FROM fyniq.document d WHERE d.organisation_id = OLD.organisation_id AND d.id = OLD.document_id;
  IF NEW.state IS DISTINCT FROM OLD.state THEN
    IF OLD.state = 'attached' AND NEW.state = 'removed' THEN
      SELECT d.status INTO parent FROM fyniq.document d
       WHERE d.organisation_id = OLD.organisation_id AND d.id = OLD.document_id FOR SHARE;
      IF parent IS DISTINCT FROM 'draft' THEN
        RAISE EXCEPTION 'Evidence % cannot be removed: its record is %, and evidence on it is append-only', OLD.id, parent
          USING ERRCODE = 'FY121', HINT = 'Files can be removed only while the record is a draft; add a newer file instead (FR-DOC-07).';
      END IF;
    ELSIF OLD.state = 'attached' AND NEW.state = 'final' THEN
      IF parent IS NOT DISTINCT FROM 'draft' THEN
        RAISE EXCEPTION 'Evidence % cannot be final while its record is a draft', OLD.id
          USING ERRCODE = 'FY122', HINT = 'Evidence is committed to the locked store when its record is posted (FR-DOC-13).';
      END IF;
    ELSE
      RAISE EXCEPTION 'Evidence % cannot go from % to %', OLD.id, OLD.state, NEW.state
        USING ERRCODE = 'FY122', HINT = 'Evidence is attached, then removed from a draft or made final; removed and final evidence stays so.';
    END IF;
  ELSIF OLD.state = 'removed' THEN
    RAISE EXCEPTION 'Evidence % was removed and stays so', OLD.id USING ERRCODE = 'FY122';
  END IF;
  IF OLD.state = 'final' AND (NEW.locked_key IS DISTINCT FROM OLD.locked_key OR NEW.finalised_at IS DISTINCT FROM OLD.finalised_at) THEN
    RAISE EXCEPTION 'Evidence % is committed: its locked copy cannot change', OLD.id USING ERRCODE = 'FY120';
  END IF;
  IF NEW.retain_until < OLD.retain_until THEN
    RAISE EXCEPTION 'Evidence % is kept until %, which cannot move earlier', OLD.id, OLD.retain_until
      USING ERRCODE = 'FY123', HINT = 'A retain-until is only ever extended (FR-DOC-02).';
  END IF;
  IF NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'Evidence % moves from version % to the next, not %', OLD.id, OLD.version, NEW.version USING ERRCODE = 'FY120';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER evidence_guard BEFORE UPDATE ON fyniq.evidence
  FOR EACH ROW EXECUTE FUNCTION fyniq.evidence_guard();

-- +goose StatementBegin
CREATE FUNCTION fyniq.evidence_never_deleted() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'Evidence is never deleted (% on %)', TG_OP, TG_TABLE_NAME
    USING ERRCODE = 'FY124', HINT = 'Remove a file from a draft record instead; committed evidence goes only at the end of its retention.';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER evidence_never_deleted BEFORE DELETE ON fyniq.evidence
  FOR EACH ROW EXECUTE FUNCTION fyniq.evidence_never_deleted();
CREATE TRIGGER evidence_no_truncate BEFORE TRUNCATE ON fyniq.evidence
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.evidence_never_deleted();

ALTER TABLE fyniq.evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.evidence
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.evidence
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('evidence', 'id');

-- The API attaches and removes from drafts; only the worker commits a file to the locked store.
GRANT SELECT ON fyniq.evidence TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (document_id, evidence_type, note, relates_on, file_name, media_type, size_bytes, sha256, original_sha256, source,
              data_class, staging_key, uploaded_by),
      UPDATE (state, removed_at, version) ON fyniq.evidence TO fyniq_api;
GRANT UPDATE (state, locked_key, retain_until, finalised_at, version) ON fyniq.evidence TO fyniq_worker;
