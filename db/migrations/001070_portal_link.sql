-- The customer portal's link (PRD-001 FR-AR-14, rev 0.512; p2-customer-portal-subscriptions task 1.1; the threat model in
-- docs/security/threat-models/customer-portal.md).
--
-- A portal link opens one customer's issued invoices, their open balances and a statement, with no session: a random 256-bit
-- token that is shown once to the person who made it and kept here only as its SHA-256. The registry portal_link_token maps a
-- hash to its organisation and link, so the public endpoints, which have no session, find the tenant through portal_link_route
-- (security definer, as document_link_route), then read the link and its customer's documents under RLS. A link names one
-- customer contact of its organisation and never changes but in its first opening and its revocation, each once (FYP10); it is
-- only of an active customer (FYP11) and is never deleted.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.portal_link (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  contact_id      uuid        NOT NULL,
  label           text,
  token_hash      text        NOT NULL,  -- the SHA-256 of the token, in hex; the token itself is never stored
  expires_at      timestamptz NOT NULL,
  created_by      text        NOT NULL,
  created_at      timestamptz NOT NULL,  -- the platform clock when it was made
  first_opened_at timestamptz,
  revoked_at      timestamptz,
  revoked_by      text,
  CONSTRAINT portal_link_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT portal_link_contact_fkey FOREIGN KEY (organisation_id, contact_id) REFERENCES fyniq.contact (organisation_id, id),
  CONSTRAINT portal_link_token_format CHECK (token_hash ~ '^[0-9a-f]{64}$'),
  CONSTRAINT portal_link_expires_later CHECK (expires_at > created_at AND expires_at <= created_at + interval '90 days'),
  CONSTRAINT portal_link_label_length CHECK (label IS NULL OR length(label) BETWEEN 1 AND 200),
  CONSTRAINT portal_link_revoked_by CHECK ((revoked_at IS NULL) = (revoked_by IS NULL)
    AND (revoked_by IS NULL OR revoked_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')),
  CONSTRAINT portal_link_created_by_format CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX portal_link_contact ON fyniq.portal_link (organisation_id, contact_id, created_at);

-- The platform's registry of portal link hashes: the organisation and link each opens. Only portal_link_route reads it.
CREATE TABLE fyniq.portal_link_token (
  token_hash text NOT NULL,
  owner      uuid NOT NULL,
  link_id    uuid NOT NULL,
  CONSTRAINT portal_link_token_pkey PRIMARY KEY (token_hash)
);
REVOKE ALL ON fyniq.portal_link_token FROM PUBLIC;

ALTER TABLE fyniq.portal_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.portal_link FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.portal_link
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.portal_link
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('portal_link', 'id');

-- A link changes only in its first opening and its revocation, each once, and never goes (FYP10); a new one is only of an active
-- customer (FYP11), and is entered in the registry.
-- +goose StatementBegin
CREATE FUNCTION fyniq.portal_link_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  c fyniq.contact;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'The portal link % is never deleted', OLD.id
      USING ERRCODE = 'FYP10', HINT = 'Revoke it instead (FR-AR-14).';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF NEW.organisation_id IS DISTINCT FROM OLD.organisation_id OR NEW.id IS DISTINCT FROM OLD.id
       OR NEW.contact_id IS DISTINCT FROM OLD.contact_id OR NEW.label IS DISTINCT FROM OLD.label
       OR NEW.token_hash IS DISTINCT FROM OLD.token_hash OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
       OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at IS DISTINCT FROM OLD.created_at
       OR (OLD.first_opened_at IS NOT NULL AND NEW.first_opened_at IS DISTINCT FROM OLD.first_opened_at)
       OR (OLD.revoked_at IS NOT NULL AND (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at OR NEW.revoked_by IS DISTINCT FROM OLD.revoked_by)) THEN
      RAISE EXCEPTION 'The portal link % changes only in its first opening and its revocation, each once', OLD.id
        USING ERRCODE = 'FYP10', HINT = 'Make a new link for the customer (FR-AR-14).';
    END IF;
    RETURN NEW;
  END IF;
  SELECT * INTO c FROM fyniq.contact WHERE organisation_id = NEW.organisation_id AND id = NEW.contact_id;
  IF NOT FOUND OR NOT c.is_customer OR NOT c.is_active THEN
    RAISE EXCEPTION 'A portal link is only of an active customer'
      USING ERRCODE = 'FYP11', HINT = 'Mark the contact as an active customer first (FR-AR-14).';
  END IF;
  INSERT INTO fyniq.portal_link_token (token_hash, owner, link_id) VALUES (NEW.token_hash, NEW.organisation_id, NEW.id);
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER portal_link_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.portal_link
  FOR EACH ROW EXECUTE FUNCTION fyniq.portal_link_guard();
CREATE TRIGGER portal_link_no_truncate BEFORE TRUNCATE ON fyniq.portal_link
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER portal_link_token_no_truncate BEFORE TRUNCATE ON fyniq.portal_link_token
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

-- A registry row never changes or goes, whoever tries (FYP10).
-- +goose StatementBegin
CREATE FUNCTION fyniq.portal_link_token_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A portal link''s registry entry never changes or goes'
    USING ERRCODE = 'FYP10', HINT = 'Revoke the link instead (FR-AR-14).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER portal_link_token_guard BEFORE UPDATE OR DELETE ON fyniq.portal_link_token
  FOR EACH ROW EXECUTE FUNCTION fyniq.portal_link_token_guard();

-- The organisation and link a token's hash opens; nothing for a hash it does not know. The public endpoints call it before any
-- tenant is set.
-- +goose StatementBegin
CREATE FUNCTION fyniq.portal_link_route(p_hash text) RETURNS TABLE (organisation_id uuid, link_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT t.owner, t.link_id FROM fyniq.portal_link_token t WHERE t.token_hash = p_hash
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.portal_link_route(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.portal_link_route(text) TO fyniq_api;
GRANT SELECT ON fyniq.portal_link TO fyniq_api, fyniq_worker, fyniq_readonly; -- the worker reads it for the tenant export, which drops the token hash
GRANT INSERT (contact_id, label, token_hash, expires_at, created_by, created_at) ON fyniq.portal_link TO fyniq_api;
GRANT UPDATE (first_opened_at, revoked_at, revoked_by) ON fyniq.portal_link TO fyniq_api;
