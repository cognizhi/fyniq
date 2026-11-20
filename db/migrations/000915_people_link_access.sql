-- One sign-in across the apps, finished (PRD-001 FR-PPL-02, FR-PPL-03, FR-PPL-18; PRD-002 NM-INT-02, NM-INT-12; p1-nemura-fyniq-link 4.1, 4.3; owner decision of 21 Nov 2026: the sign-in is one
-- identity-provider tenant, neither app is the other's provider).
--
-- people_link_access is what Nemura says of each sign-in identity it knows (identity.access.changed, signed with the link's key, through the projection's door): whether the identity is an active member
-- of the organisation there (people_access) and whether it holds a role that approves a pay run (payroll_approver). A door and a hint, never a grant: it decides whether the Finance | People switch shows
-- and whether the person is asked to enrol the passkey Nemura's approval is signed with, and nothing else. It holds no role name and no email. The version is the sending event's sequence, so a newer
-- answer replaces an older one and an older one arriving late changes nothing.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.people_link_access (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  issuer           text        NOT NULL,
  subject          text        NOT NULL,
  people_access    boolean     NOT NULL,   -- an active member of the organisation in Nemura
  payroll_approver boolean     NOT NULL,   -- holds a role that approves a pay run there
  version          bigint      NOT NULL,   -- the sending event's sequence
  received_at      timestamptz NOT NULL,
  CONSTRAINT people_link_access_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT people_link_access_identity_key UNIQUE (organisation_id, issuer, subject),
  CONSTRAINT people_link_access_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT people_link_access_issuer CHECK (char_length(issuer) BETWEEN 1 AND 500),
  CONSTRAINT people_link_access_subject CHECK (char_length(subject) BETWEEN 1 AND 500),
  CONSTRAINT people_link_access_version CHECK (version >= 1)
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.people_link_access_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.id IS DISTINCT FROM OLD.id OR NEW.issuer IS DISTINCT FROM OLD.issuer OR NEW.subject IS DISTINCT FROM OLD.subject OR NEW.version <= OLD.version THEN
    RAISE EXCEPTION 'What Nemura says of an identity is replaced by a newer version, never changed back or deleted' USING ERRCODE = 'FY900',
      HINT = 'The version is the sending event''s sequence (FR-PPL-15).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER people_link_access_guard BEFORE UPDATE OR DELETE ON fyniq.people_link_access
  FOR EACH ROW EXECUTE FUNCTION fyniq.people_link_access_guard();
CREATE TRIGGER people_link_access_no_truncate BEFORE TRUNCATE ON fyniq.people_link_access
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.people_link_access
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('people_link_access', 'id');

ALTER TABLE fyniq.people_link_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.people_link_access FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.people_link_access
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.people_link_access FROM PUBLIC;
GRANT SELECT ON fyniq.people_link_access TO fyniq_api, fyniq_worker, fyniq_readonly;
-- It arrives through the inbox, which the API runs.
GRANT INSERT (id, issuer, subject, people_access, payroll_approver, version, received_at), UPDATE (people_access, payroll_approver, version, received_at) ON fyniq.people_link_access TO fyniq_api;
