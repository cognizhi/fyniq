-- Data protection impact assessments (PRD-001 CMP-PD-08, rev 0.244; p1-pdpa-retention slice 5, design D4). Fyniq drafts
-- the payroll link's and the AI assistant's; the Compliance officer adopts or edits them, or writes one for any other
-- processing, each saved as versions, and approves a version, stepped up. Versions and approvals are only ever added
-- (FY313), each audited in the same transaction (AUD-01), under the "Data protection (PDPA)" row of Appendix D.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.dpia_version (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  key             text        NOT NULL,  -- payroll, ai-assistant, or other-<uuid> for the organisation's own
  version         integer     NOT NULL,
  title           text        NOT NULL,
  body            text        NOT NULL,
  sha256          bytea       NOT NULL,
  from_draft      text        NOT NULL,  -- fyniq-dpia-draft@1, or organisation
  entry_id        uuid,                  -- the inventory entry it assesses, if any
  saved_by        text        NOT NULL,
  saved_at        timestamptz NOT NULL,
  CONSTRAINT dpia_version_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT dpia_version_once UNIQUE (organisation_id, key, version),
  CONSTRAINT dpia_version_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT dpia_version_key_format CHECK (key ~ '^(payroll|ai-assistant|other-[0-9a-f-]{36})$'),
  CONSTRAINT dpia_version_positive CHECK (version > 0),
  CONSTRAINT dpia_version_title_length CHECK (length(btrim(title)) BETWEEN 1 AND 200),
  CONSTRAINT dpia_version_body_length CHECK (length(btrim(body)) BETWEEN 1 AND 100000),
  CONSTRAINT dpia_version_sha256 CHECK (sha256 = sha256(convert_to(body, 'UTF8'))),
  CONSTRAINT dpia_version_from_format CHECK (from_draft ~ '^(fyniq-dpia-draft@[0-9]+|organisation)$'),
  CONSTRAINT dpia_version_saved_by_format CHECK (saved_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

CREATE TABLE fyniq.dpia_approval (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  key             text        NOT NULL,
  version         integer     NOT NULL,
  approved_by     text        NOT NULL,
  approved_at     timestamptz NOT NULL,
  CONSTRAINT dpia_approval_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT dpia_approval_once UNIQUE (organisation_id, key, version),
  CONSTRAINT dpia_approval_version_fkey FOREIGN KEY (organisation_id, key, version) REFERENCES fyniq.dpia_version (organisation_id, key, version),
  CONSTRAINT dpia_approval_by_format CHECK (approved_by ~ '^user:[!-~]{1,200}$')
);

-- A version follows the assessment's last, and an approval is of its latest version; nothing changes or goes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.dpia_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  latest integer;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'An impact assessment''s versions and approvals never change or go' USING ERRCODE = 'FY313',
      HINT = 'Save the assessment''s next version and approve it (CMP-PD-08).';
  END IF;
  SELECT max(v.version) INTO latest FROM fyniq.dpia_version v WHERE v.organisation_id = NEW.organisation_id AND v.key = NEW.key;
  IF TG_TABLE_NAME = 'dpia_version' AND NEW.version <> coalesce(latest, 0) + 1 THEN
    RAISE EXCEPTION 'Version % of an impact assessment follows its version %', NEW.version, coalesce(latest, 0) USING ERRCODE = 'FY313';
  END IF;
  IF TG_TABLE_NAME = 'dpia_approval' AND NEW.version <> latest THEN
    RAISE EXCEPTION 'Only an impact assessment''s latest version, %, is approved', latest USING ERRCODE = 'FY313';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER dpia_version_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.dpia_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.dpia_guard();
CREATE TRIGGER dpia_approval_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.dpia_approval
  FOR EACH ROW EXECUTE FUNCTION fyniq.dpia_guard();
CREATE TRIGGER dpia_version_no_truncate BEFORE TRUNCATE ON fyniq.dpia_version
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER dpia_approval_no_truncate BEFORE TRUNCATE ON fyniq.dpia_approval
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.dpia_version
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('dpia_version', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.dpia_approval
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('dpia_approval', 'id');

ALTER TABLE fyniq.dpia_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.dpia_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.dpia_version
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.dpia_approval ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.dpia_approval FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.dpia_approval
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (key, version, title, body, sha256, from_draft, entry_id, saved_by, saved_at) ON fyniq.dpia_version TO fyniq_api;
GRANT INSERT (key, version, approved_by, approved_at) ON fyniq.dpia_approval TO fyniq_api;
GRANT SELECT ON fyniq.dpia_version, fyniq.dpia_approval TO fyniq_api, fyniq_worker, fyniq_readonly;
