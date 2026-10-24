-- The online link to a quote or invoice sent by email (PRD-001 FR-AR-06, FR-AR-16; p1-sales task 5.3; the threat
-- model in docs/security/threat-models/document-links.md).
--
-- Emailing a sent quote or an issued invoice makes a link for that version: a random 256-bit token the email carries,
-- kept here only as its SHA-256. The registry document_link_token maps a hash to its organisation and link, so the
-- public read endpoint, which has no session, finds the tenant through document_link_route (security definer, as
-- email-in's capture_route), then reads the link and its document under RLS. A link never changes but in its first
-- view and its revocation, each once, and is never deleted (FY197); it is only of a posted quote, sales order or
-- invoice at its current version, through the email message that carried it (FY198).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.document_link (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  document_id      uuid        NOT NULL,
  document_version integer     NOT NULL,
  message_id       uuid        NOT NULL,
  token_hash       text        NOT NULL,  -- the SHA-256 of the token, in hex; the token itself is never stored
  expires_at       timestamptz NOT NULL,
  created_by       text        NOT NULL,
  created_at       timestamptz NOT NULL DEFAULT now(),
  first_viewed_at  timestamptz,
  revoked_at       timestamptz,
  revoked_by       text,
  CONSTRAINT document_link_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT document_link_version_fkey FOREIGN KEY (organisation_id, document_id, document_version)
    REFERENCES fyniq.document_version (organisation_id, document_id, version),
  CONSTRAINT document_link_message_fkey FOREIGN KEY (organisation_id, message_id) REFERENCES fyniq.email_message (organisation_id, id),
  CONSTRAINT document_link_message_once UNIQUE (organisation_id, message_id),
  CONSTRAINT document_link_token_format CHECK (token_hash ~ '^[0-9a-f]{64}$'),
  CONSTRAINT document_link_expires_later CHECK (expires_at > created_at),
  CONSTRAINT document_link_revoked_by CHECK ((revoked_at IS NULL) = (revoked_by IS NULL)
    AND (revoked_by IS NULL OR revoked_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')),
  CONSTRAINT document_link_created_by_format CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX document_link_document ON fyniq.document_link (organisation_id, document_id, created_at);

-- The platform's registry of link hashes: the organisation and link each opens. Only document_link_route reads it.
CREATE TABLE fyniq.document_link_token (
  token_hash text NOT NULL,
  owner      uuid NOT NULL,
  link_id    uuid NOT NULL,
  CONSTRAINT document_link_token_pkey PRIMARY KEY (token_hash)
);
REVOKE ALL ON fyniq.document_link_token FROM PUBLIC;

ALTER TABLE fyniq.document_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.document_link FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.document_link
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.document_link
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document_link', 'id');

-- A link changes only in its first view and its revocation, each once, and never goes (FY197); a new one is only of
-- a posted quote, sales order or invoice at its current version (FY198), and is entered in the registry.
-- +goose StatementBegin
CREATE FUNCTION fyniq.document_link_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  doc fyniq.document;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'The link % of document % is never deleted', OLD.id, OLD.document_id
      USING ERRCODE = 'FY197', HINT = 'Revoke it instead (FR-AR-06).';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF NEW.organisation_id IS DISTINCT FROM OLD.organisation_id OR NEW.id IS DISTINCT FROM OLD.id
       OR NEW.document_id IS DISTINCT FROM OLD.document_id OR NEW.document_version IS DISTINCT FROM OLD.document_version
       OR NEW.message_id IS DISTINCT FROM OLD.message_id OR NEW.token_hash IS DISTINCT FROM OLD.token_hash
       OR NEW.expires_at IS DISTINCT FROM OLD.expires_at OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at
       OR (OLD.first_viewed_at IS NOT NULL AND NEW.first_viewed_at IS DISTINCT FROM OLD.first_viewed_at)
       OR (OLD.revoked_at IS NOT NULL AND (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at OR NEW.revoked_by IS DISTINCT FROM OLD.revoked_by)) THEN
      RAISE EXCEPTION 'The link % changes only in its first view and its revocation, each once', OLD.id
        USING ERRCODE = 'FY197', HINT = 'Make a new link by sending the document again (FR-AR-06).';
    END IF;
    RETURN NEW;
  END IF;
  SELECT * INTO doc FROM fyniq.document WHERE organisation_id = NEW.organisation_id AND id = NEW.document_id;
  IF doc.type NOT IN ('quote', 'sales_order', 'invoice') OR doc.status <> 'posted' OR doc.current_version <> NEW.document_version THEN
    RAISE EXCEPTION 'A link is only of the current version of a sent quote or an issued invoice, not version % of a % that is %',
      NEW.document_version, doc.type, doc.status
      USING ERRCODE = 'FY198', HINT = 'Issue or send it first (FR-AR-06).';
  END IF;
  INSERT INTO fyniq.document_link_token (token_hash, owner, link_id) VALUES (NEW.token_hash, NEW.organisation_id, NEW.id);
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER document_link_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.document_link
  FOR EACH ROW EXECUTE FUNCTION fyniq.document_link_guard();
CREATE TRIGGER document_link_no_truncate BEFORE TRUNCATE ON fyniq.document_link
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER document_link_token_no_truncate BEFORE TRUNCATE ON fyniq.document_link_token
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

-- A registry row never changes or goes, whoever tries (FY197).
-- +goose StatementBegin
CREATE FUNCTION fyniq.document_link_token_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A link''s registry entry never changes or goes'
    USING ERRCODE = 'FY197', HINT = 'Revoke the link instead (FR-AR-06).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER document_link_token_guard BEFORE UPDATE OR DELETE ON fyniq.document_link_token
  FOR EACH ROW EXECUTE FUNCTION fyniq.document_link_token_guard();

-- The organisation and link a token's hash opens; nothing for a hash it does not know. The public read endpoint calls
-- it before any tenant is set.
-- +goose StatementBegin
CREATE FUNCTION fyniq.document_link_route(p_hash text) RETURNS TABLE (organisation_id uuid, link_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT t.owner, t.link_id FROM fyniq.document_link_token t WHERE t.token_hash = p_hash
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.document_link_route(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.document_link_route(text) TO fyniq_api;
GRANT SELECT ON fyniq.document_link TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (document_id, document_version, message_id, token_hash, expires_at, created_by) ON fyniq.document_link TO fyniq_api;
GRANT UPDATE (first_viewed_at, revoked_at, revoked_by) ON fyniq.document_link TO fyniq_api;
