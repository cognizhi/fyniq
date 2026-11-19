-- Nemura's own API clients (PRD-002 NM-ARC-06, question 11: Nemura's own token issuer, so standalone needs nothing from Fyniq).
--
-- An API client is an integration: a system that calls Nemura's API on its own with the client-credentials grant, authenticating with a key pair it holds
-- (private_key_jwt, never a shared secret), for the API's audience and the scopes it was registered with. Ory Hydra holds the client and issues its
-- tokens; this record decides what its tokens may do and who registered it. Registering and revoking are audited.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.api_client (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  name            text        NOT NULL,
  scopes          text[]      NOT NULL,
  hydra_client_id text        NOT NULL,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  revoked_at      timestamptz,
  CONSTRAINT api_client_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT api_client_hydra_key UNIQUE (hydra_client_id),
  CONSTRAINT api_client_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT api_client_created_by_fkey FOREIGN KEY (organisation_id, created_by)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT api_client_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 100),
  CONSTRAINT api_client_scopes_some CHECK (cardinality(scopes) BETWEEN 1 AND 20)
);
-- A live client's name is unique in its organisation: a repeat of a registration is refused, not done twice.
CREATE UNIQUE INDEX api_client_live_name_key ON nemura.api_client (organisation_id, lower(name)) WHERE revoked_at IS NULL;

-- A client's record never changes except to be revoked, once; nothing is deleted.
-- +goose StatementBegin
CREATE FUNCTION nemura.api_client_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.name IS DISTINCT FROM OLD.name OR NEW.scopes IS DISTINCT FROM OLD.scopes OR NEW.hydra_client_id IS DISTINCT FROM OLD.hydra_client_id
     OR NEW.created_by IS DISTINCT FROM OLD.created_by OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
    RAISE EXCEPTION 'An API client can only be revoked, once' USING ERRCODE = 'NM104';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER api_client_guard BEFORE UPDATE OR DELETE ON nemura.api_client
  FOR EACH ROW EXECUTE FUNCTION nemura.api_client_guard();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.api_client
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('api_client', 'id');

ALTER TABLE nemura.api_client ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.api_client FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.api_client
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.api_client TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, name, scopes, hydra_client_id, created_by), UPDATE (revoked_at) ON nemura.api_client TO nemura_api;
