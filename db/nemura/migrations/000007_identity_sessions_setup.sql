-- Who a person is, and the sessions they hold (PRD-002 NM-ARC-01, NM-ARC-06, ADR-N3; NM-INT-12's rule for the link change).
--
-- People are matched by the identity provider's issuer, subject and tenant, never by email alone, and one person may belong to several
-- organisations. The directory that finds a person's memberships before any organisation is known is a global table that no
-- application role can read: services reach it only through the SECURITY DEFINER functions below, which return nothing but the
-- caller's own memberships. First-run setup is done once, with a one-time code created at deployment and kept as its SHA-256. A
-- session is an opaque token kept as its SHA-256, with an expiry; a role change or a deactivation takes effect on the next request.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- ------------------------------------------------------------------ global: the installation's first-run setup
CREATE TABLE nemura.installation (
  singleton          boolean     NOT NULL DEFAULT true,
  setup_code_hash    bytea,
  setup_completed_at timestamptz,
  CONSTRAINT installation_pkey PRIMARY KEY (singleton),
  CONSTRAINT installation_one_row CHECK (singleton),
  CONSTRAINT installation_code_sha256 CHECK (length(setup_code_hash) = 32),
  -- A code waits for setup, or setup is done; never both.
  CONSTRAINT installation_code_or_done CHECK ((setup_code_hash IS NULL) <> (setup_completed_at IS NULL))
);

-- The migrator sets the code at deployment (a new code replaces an unused one; once setup is done it is refused).
-- +goose StatementBegin
CREATE FUNCTION nemura.set_setup_code(p_hash bytea) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM nemura.installation WHERE setup_completed_at IS NOT NULL) THEN
    RAISE EXCEPTION 'Setup is already done' USING ERRCODE = 'NM100', HINT = 'The first-run setup code works once.';
  END IF;
  INSERT INTO nemura.installation (singleton, setup_code_hash) VALUES (true, p_hash)
    ON CONFLICT (singleton) DO UPDATE SET setup_code_hash = EXCLUDED.setup_code_hash;
END
$$;
-- +goose StatementEnd

-- Whether setup is done: the sign-in page shows the setup form until it is.
-- +goose StatementBegin
CREATE FUNCTION nemura.setup_done() RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$ SELECT coalesce((SELECT setup_completed_at IS NOT NULL FROM nemura.installation), false) $$;
-- +goose StatementEnd

-- Claims the setup code, once: true when the code was the right one and unused, atomically with the work the caller does in the
-- same transaction (the first organisation, its first Owner and their identity).
-- +goose StatementBegin
CREATE FUNCTION nemura.claim_setup(p_hash bytea) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  n integer;
BEGIN
  UPDATE nemura.installation SET setup_code_hash = NULL, setup_completed_at = now()
   WHERE setup_code_hash = p_hash AND setup_completed_at IS NULL;
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n = 1;
END
$$;
-- +goose StatementEnd

-- ------------------------------------------------------------------ global: the identity directory
CREATE TABLE nemura.identity_directory (
  issuer          text        NOT NULL,
  subject         text        NOT NULL,
  tenant          text        NOT NULL,
  organisation_id uuid        NOT NULL,
  member_id       uuid        NOT NULL,
  linked_at       timestamptz NOT NULL DEFAULT now(),
  active          boolean     NOT NULL DEFAULT true,   -- false once the member is deactivated: the directory is read with no organisation set
  CONSTRAINT identity_directory_pkey PRIMARY KEY (issuer, subject, tenant, organisation_id),
  CONSTRAINT identity_directory_member_fkey FOREIGN KEY (organisation_id, member_id)
    REFERENCES nemura.member (organisation_id, id),
  -- one identity is one member of an organisation, and a member has one identity per issuer and tenant
  CONSTRAINT identity_directory_member_key UNIQUE (organisation_id, member_id, issuer, tenant),
  CONSTRAINT identity_directory_issuer_length CHECK (length(issuer) BETWEEN 1 AND 500),
  CONSTRAINT identity_directory_subject_length CHECK (length(subject) BETWEEN 1 AND 500),
  CONSTRAINT identity_directory_tenant_length CHECK (length(tenant) BETWEEN 1 AND 500)
);

-- Links an identity to a member of the transaction's organisation. Never by email: the key is issuer, subject and tenant.
-- +goose StatementBegin
CREATE FUNCTION nemura.link_identity(p_issuer text, p_subject text, p_tenant text, p_member uuid) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  INSERT INTO nemura.identity_directory (issuer, subject, tenant, organisation_id, member_id)
  VALUES (p_issuer, p_subject, p_tenant, nemura.require_org(), p_member);
END
$$;
-- +goose StatementEnd

-- The memberships of an identity, across organisations, for sign-in before an organisation is chosen. Deactivated members are
-- not returned: a deactivated person cannot sign in.
-- +goose StatementBegin
CREATE FUNCTION nemura.members_for_identity(p_issuer text, p_subject text, p_tenant text)
RETURNS TABLE (organisation_id uuid, member_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT d.organisation_id, d.member_id
    FROM nemura.identity_directory d
   WHERE d.issuer = p_issuer AND d.subject = p_subject AND d.tenant = p_tenant AND d.active
   ORDER BY d.linked_at
$$;
-- +goose StatementEnd

-- Deactivating a member switches their identities off in the directory, in the same transaction.
-- +goose StatementBegin
CREATE FUNCTION nemura.member_deactivated() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  UPDATE nemura.identity_directory SET active = (NEW.deactivated_at IS NULL) WHERE organisation_id = NEW.organisation_id AND member_id = NEW.id;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER member_deactivated AFTER UPDATE OF deactivated_at ON nemura.member
  FOR EACH ROW EXECUTE FUNCTION nemura.member_deactivated();

-- ------------------------------------------------------------------ tenant: invitations
CREATE TABLE nemura.invitation (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  member_id       uuid        NOT NULL,
  code_hash       bytea       NOT NULL,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  expires_at      timestamptz NOT NULL,
  accepted_at     timestamptz,
  CONSTRAINT invitation_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT invitation_code_key UNIQUE (organisation_id, code_hash),
  CONSTRAINT invitation_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT invitation_member_fkey FOREIGN KEY (organisation_id, member_id)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT invitation_created_by_fkey FOREIGN KEY (organisation_id, created_by)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT invitation_code_sha256 CHECK (length(code_hash) = 32),
  CONSTRAINT invitation_expiry CHECK (expires_at > created_at)
);

-- ------------------------------------------------------------------ tenant: sessions
-- The token is opaque and never kept: only its SHA-256. A session ends at its expiry or when revoked; it carries when the person last
-- confirmed it with a passkey (step-up).
CREATE TABLE nemura.session (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  member_id       uuid        NOT NULL,
  token_hash      bytea       NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  expires_at      timestamptz NOT NULL,
  revoked_at      timestamptz,
  step_up_at      timestamptz,
  CONSTRAINT session_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT session_token_key UNIQUE (organisation_id, token_hash),
  CONSTRAINT session_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT session_member_fkey FOREIGN KEY (organisation_id, member_id)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT session_token_sha256 CHECK (length(token_hash) = 32),
  CONSTRAINT session_expiry CHECK (expires_at > created_at)
);
CREATE INDEX session_member_idx ON nemura.session (organisation_id, member_id);

-- A session only ends: revoked once, or its step-up recorded; its member and token never change, and nothing is deleted.
-- +goose StatementBegin
CREATE FUNCTION nemura.session_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.member_id IS DISTINCT FROM OLD.member_id OR NEW.token_hash IS DISTINCT FROM OLD.token_hash
     OR NEW.expires_at IS DISTINCT FROM OLD.expires_at OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
    RAISE EXCEPTION 'A session can only be revoked, once, or confirmed again' USING ERRCODE = 'NM101';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER session_guard BEFORE UPDATE OR DELETE ON nemura.session
  FOR EACH ROW EXECUTE FUNCTION nemura.session_guard();

-- ------------------------------------------------------------------ audit, row-level security, grants
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.invitation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('invitation', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.session
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('session', 'id');

ALTER TABLE nemura.invitation ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.invitation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.invitation
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.session ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.session FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.session
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.invitation, nemura.session TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, member_id, code_hash, created_by, expires_at), UPDATE (accepted_at) ON nemura.invitation TO nemura_api;
GRANT INSERT (id, member_id, token_hash, expires_at), UPDATE (revoked_at, step_up_at) ON nemura.session TO nemura_api, nemura_worker;

-- The global functions: the migrator sets the code; the api claims it and asks about identities; nothing else.
REVOKE ALL ON FUNCTION nemura.set_setup_code(bytea), nemura.setup_done(), nemura.claim_setup(bytea),
  nemura.link_identity(text, text, text, uuid), nemura.members_for_identity(text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.setup_done(), nemura.claim_setup(bytea), nemura.link_identity(text, text, text, uuid),
  nemura.members_for_identity(text, text, text) TO nemura_api;
