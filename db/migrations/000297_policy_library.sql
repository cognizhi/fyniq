-- The policy library (PRD-001 CMP-07; Appendix D "Policy library"; p1-compliance-center slice 7, design D8).
--
-- Fyniq ships a starting draft of each of the eight policies (compliance/policies/). An Owner or the Compliance officer
-- adopts a draft as it is, or edits it: each edit is a revision of the organisation's draft of that policy. Publishing
-- makes the draft (Fyniq's, or the latest revision) the policy's next version, with its text and SHA-256; a published
-- version never changes. Every member acknowledges each published version for themselves, once. All three are only ever
-- added (FY310), each audited in the same transaction (AUD-01).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.policy_draft (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  key             text        NOT NULL,
  revision        integer     NOT NULL,
  body            text        NOT NULL,
  sha256          bytea       NOT NULL,
  saved_by        text        NOT NULL,  -- the actor, as records name it: user:<id>
  saved_at        timestamptz NOT NULL,
  CONSTRAINT policy_draft_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT policy_draft_once UNIQUE (organisation_id, key, revision),
  CONSTRAINT policy_draft_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT policy_draft_key_known CHECK (key IN ('information-security', 'acceptable-use', 'access-control', 'incident-response',
    'backup', 'change-management', 'ai-use', 'data-protection')),
  CONSTRAINT policy_draft_revision_positive CHECK (revision > 0),
  CONSTRAINT policy_draft_body_length CHECK (length(btrim(body)) BETWEEN 1 AND 100000),
  CONSTRAINT policy_draft_sha256 CHECK (sha256 = sha256(convert_to(body, 'UTF8'))),
  CONSTRAINT policy_draft_saved_by_format CHECK (saved_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

CREATE TABLE fyniq.policy_version (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  key             text        NOT NULL,
  version         integer     NOT NULL,
  title           text        NOT NULL,
  body            text        NOT NULL,
  sha256          bytea       NOT NULL,
  from_draft      text        NOT NULL,  -- fyniq-policy-draft@1, or the organisation's revision it was published from
  published_by    text        NOT NULL,
  published_at    timestamptz NOT NULL,
  CONSTRAINT policy_version_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT policy_version_once UNIQUE (organisation_id, key, version),
  CONSTRAINT policy_version_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT policy_version_key_known CHECK (key IN ('information-security', 'acceptable-use', 'access-control', 'incident-response',
    'backup', 'change-management', 'ai-use', 'data-protection')),
  CONSTRAINT policy_version_positive CHECK (version > 0),
  CONSTRAINT policy_version_title_length CHECK (length(btrim(title)) BETWEEN 1 AND 200),
  CONSTRAINT policy_version_body_length CHECK (length(btrim(body)) BETWEEN 1 AND 100000),
  CONSTRAINT policy_version_sha256 CHECK (sha256 = sha256(convert_to(body, 'UTF8'))),
  CONSTRAINT policy_version_from_format CHECK (from_draft ~ '^(fyniq-policy-draft@[0-9]+|revision [0-9]+)$'),
  CONSTRAINT policy_version_published_by_format CHECK (published_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

CREATE TABLE fyniq.policy_acknowledgement (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  key             text        NOT NULL,
  version         integer     NOT NULL,
  acknowledged_by text        NOT NULL,  -- the member, as records name actors: user:<id>
  acknowledged_at timestamptz NOT NULL,
  CONSTRAINT policy_acknowledgement_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT policy_acknowledgement_once UNIQUE (organisation_id, key, version, acknowledged_by),
  CONSTRAINT policy_acknowledgement_version_fkey FOREIGN KEY (organisation_id, key, version)
    REFERENCES fyniq.policy_version (organisation_id, key, version),
  CONSTRAINT policy_acknowledgement_by_format CHECK (acknowledged_by ~ '^user:[!-~]{1,200}$')
);

-- A draft's revision and a version are the policy's next; nothing of the library ever changes or goes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.policy_library_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  latest integer;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The policy library''s drafts, versions and acknowledgements never change or go' USING ERRCODE = 'FY310',
      HINT = 'Save a new draft and publish it: that is the policy''s next version (CMP-07).';
  END IF;
  IF TG_TABLE_NAME = 'policy_draft' THEN
    SELECT max(d.revision) INTO latest FROM fyniq.policy_draft d WHERE d.organisation_id = NEW.organisation_id AND d.key = NEW.key;
    IF NEW.revision <> coalesce(latest, 0) + 1 THEN
      RAISE EXCEPTION 'Revision % of a policy''s draft follows its revision %', NEW.revision, coalesce(latest, 0) USING ERRCODE = 'FY310';
    END IF;
  ELSIF TG_TABLE_NAME = 'policy_version' THEN
    SELECT max(v.version) INTO latest FROM fyniq.policy_version v WHERE v.organisation_id = NEW.organisation_id AND v.key = NEW.key;
    IF NEW.version <> coalesce(latest, 0) + 1 THEN
      RAISE EXCEPTION 'Version % of a policy follows its version %', NEW.version, coalesce(latest, 0) USING ERRCODE = 'FY310';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER policy_draft_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.policy_draft
  FOR EACH ROW EXECUTE FUNCTION fyniq.policy_library_guard();
CREATE TRIGGER policy_version_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.policy_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.policy_library_guard();
CREATE TRIGGER policy_acknowledgement_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.policy_acknowledgement
  FOR EACH ROW EXECUTE FUNCTION fyniq.policy_library_guard();
CREATE TRIGGER policy_draft_no_truncate BEFORE TRUNCATE ON fyniq.policy_draft
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER policy_version_no_truncate BEFORE TRUNCATE ON fyniq.policy_version
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER policy_acknowledgement_no_truncate BEFORE TRUNCATE ON fyniq.policy_acknowledgement
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.policy_draft
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('policy_draft', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.policy_version
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('policy_version', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.policy_acknowledgement
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('policy_acknowledgement', 'id');

ALTER TABLE fyniq.policy_draft ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.policy_draft FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.policy_draft
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.policy_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.policy_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.policy_version
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.policy_acknowledgement ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.policy_acknowledgement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.policy_acknowledgement
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (key, revision, body, sha256, saved_by, saved_at) ON fyniq.policy_draft TO fyniq_api;
GRANT INSERT (key, version, title, body, sha256, from_draft, published_by, published_at) ON fyniq.policy_version TO fyniq_api;
GRANT INSERT (key, version, acknowledged_by, acknowledged_at) ON fyniq.policy_acknowledgement TO fyniq_api;
GRANT SELECT ON fyniq.policy_draft, fyniq.policy_version, fyniq.policy_acknowledgement TO fyniq_api, fyniq_worker, fyniq_readonly;
