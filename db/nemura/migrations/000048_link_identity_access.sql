-- One sign-in across the apps, finished (PRD-002 NM-INT-02, NM-INT-12; PRD-001 FR-PPL-02, FR-PPL-03, FR-PPL-18; p1-nemura-fyniq-link 4.1, 4.3; owner decision of 21 Nov 2026: the single sign-in is one
-- identity-provider tenant, neither app is the other's provider).
--
--  * A session records how it was signed in: the provider (so the switch to Fyniq asks the same provider, with no sign-in page in between) and the identity it proved (issuer and subject). A member can be
--    linked to more than one identity (one per issuer), so the identity a pay run's approver is named by is the one of the session that signs it, never "the earliest linked". Sessions made before this
--    carry none and fall back to the earliest. A session's way in never changes.
--  * member_identities lists every identity of a member of the transaction's own organisation, for what is queued about them (the directory itself stays unreadable to the API).
--  * link_identity_access is what Fyniq says of each identity it knows (identity.access.changed): whether they hold a role in Fyniq's finance workspace and whether a passkey is enrolled there. It is
--    a door and a hint, never a grant: it decides whether the Finance | People switch shows and whether the approval page warns that the approver's passkey is missing, and nothing else. Newest version
--    wins (the version is the event's sequence); it holds no role name and no email.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.session ADD COLUMN provider text, ADD COLUMN issuer text, ADD COLUMN subject text;
ALTER TABLE nemura.session ADD CONSTRAINT session_way_in CHECK (
  (provider IS NULL AND issuer IS NULL AND subject IS NULL)
  OR (char_length(provider) BETWEEN 1 AND 64 AND char_length(issuer) BETWEEN 1 AND 500 AND char_length(subject) BETWEEN 1 AND 500)) NOT VALID;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.session_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.member_id IS DISTINCT FROM OLD.member_id OR NEW.token_hash IS DISTINCT FROM OLD.token_hash
     OR NEW.expires_at IS DISTINCT FROM OLD.expires_at OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at)
     OR NEW.provider IS DISTINCT FROM OLD.provider OR NEW.issuer IS DISTINCT FROM OLD.issuer OR NEW.subject IS DISTINCT FROM OLD.subject THEN
    RAISE EXCEPTION 'A session can only be revoked, once, or confirmed again' USING ERRCODE = 'NM101';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT INSERT (provider, issuer, subject) ON nemura.session TO nemura_api, nemura_worker;

-- +goose StatementBegin
CREATE FUNCTION nemura.member_identities(p_member uuid) RETURNS TABLE (issuer text, subject text)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT d.issuer, d.subject
    FROM nemura.identity_directory d
   WHERE d.organisation_id = nemura.require_org() AND d.member_id = p_member
   ORDER BY d.linked_at, d.issuer, d.subject
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION nemura.member_identities(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.member_identities(uuid) TO nemura_api, nemura_worker;

CREATE TABLE nemura.link_identity_access (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  issuer          text        NOT NULL,
  subject         text        NOT NULL,
  finance         boolean     NOT NULL,   -- the identity holds a role in Fyniq's finance workspace
  passkey         boolean     NOT NULL,   -- the identity has a passkey enrolled in Fyniq (what a pay run's approval is signed with)
  version         bigint      NOT NULL,   -- the sending event's sequence: a newer one replaces an older, never the other way round
  received_at     timestamptz NOT NULL,
  CONSTRAINT link_identity_access_pkey PRIMARY KEY (organisation_id, issuer, subject),
  CONSTRAINT link_identity_access_issuer CHECK (char_length(issuer) BETWEEN 1 AND 500),
  CONSTRAINT link_identity_access_subject CHECK (char_length(subject) BETWEEN 1 AND 500),
  CONSTRAINT link_identity_access_version CHECK (version >= 1)
);
-- +goose StatementBegin
CREATE FUNCTION nemura.link_identity_access_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.issuer IS DISTINCT FROM OLD.issuer OR NEW.subject IS DISTINCT FROM OLD.subject OR NEW.version <= OLD.version THEN
    RAISE EXCEPTION 'What Fyniq says of an identity is replaced by a newer version, never changed back or deleted' USING ERRCODE = 'NM193';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER link_identity_access_guard BEFORE UPDATE OR DELETE ON nemura.link_identity_access FOR EACH ROW EXECUTE FUNCTION nemura.link_identity_access_guard();
ALTER TABLE nemura.link_identity_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.link_identity_access FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.link_identity_access USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
REVOKE ALL ON nemura.link_identity_access FROM PUBLIC;
GRANT SELECT ON nemura.link_identity_access TO nemura_api, nemura_worker, nemura_readonly;
-- It arrives through the inbox, which the API runs.
GRANT INSERT (issuer, subject, finance, passkey, version, received_at), UPDATE (finance, passkey, version, received_at) ON nemura.link_identity_access TO nemura_api;
