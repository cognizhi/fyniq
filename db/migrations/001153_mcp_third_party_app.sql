-- Third-party AI apps registered by an Administrator (PRD-001 AI-46, rev 0.608; p2-api-third-parties task 5, design D6): the organisation's own record of each
-- AI app (an MCP client of another publisher) its Administrator registered by hand. The app itself is an agent-class public client with PKCE and the default read
-- scope, recorded among the installation's registrations (mcp_client, AI-90) and unusable until an Owner adds it to the apps that may connect (mcp.clients, a dual
-- controlled setting): this table adds what AI-46 asks for beyond that, who publishes it, what for, where it processes data, the redirect URIs an Administrator
-- checked, and the data inventory entry made with it. Nothing is fetched from anything the Administrator names. A record is never changed once made but to note
-- when and by whom it was removed; nothing is deleted. Every change is audited in its transaction (AUD-01).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.mcp_third_party_app (
  organisation_id      uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                   uuid        NOT NULL DEFAULT uuidv7(),
  client_id            text        NOT NULL,
  name                 text        NOT NULL,
  publisher            text        NOT NULL,              -- who publishes it, as the Administrator verified it; shown, never fetched
  publisher_url        text,                              -- the publisher's own address, https; shown, never fetched
  redirect_uris        text[]      NOT NULL,
  purpose              text        NOT NULL,              -- what the organisation uses it for, for the data inventory
  processing_location  text        NOT NULL,              -- where the publisher processes what it reads
  outside_singapore    boolean     NOT NULL,              -- a transfer under the PDPA to assess before an Owner allows it (CMP-PD-06)
  inventory_entry      uuid        NOT NULL,              -- the data inventory entry made with the registration (CMP-PD-02)
  registered_by        uuid        NOT NULL,
  registered_at        timestamptz NOT NULL,
  removed_at           timestamptz,
  removed_by           uuid,
  CONSTRAINT mcp_third_party_app_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT mcp_third_party_app_client_once UNIQUE (client_id),
  CONSTRAINT mcp_third_party_app_registered_by_fkey FOREIGN KEY (registered_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT mcp_third_party_app_removed_by_fkey FOREIGN KEY (removed_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT mcp_third_party_app_client CHECK (client_id ~ '^mcp-[0-9a-f]{20}$'),
  CONSTRAINT mcp_third_party_app_words CHECK (length(btrim(name)) BETWEEN 1 AND 100 AND length(btrim(publisher)) BETWEEN 1 AND 100
    AND length(btrim(purpose)) BETWEEN 1 AND 300 AND length(btrim(processing_location)) BETWEEN 1 AND 100),
  CONSTRAINT mcp_third_party_app_url CHECK (publisher_url IS NULL OR (publisher_url ~ '^https://[^[:space:]]+$' AND length(publisher_url) <= 2048)),
  CONSTRAINT mcp_third_party_app_uris CHECK (cardinality(redirect_uris) BETWEEN 1 AND 5),
  CONSTRAINT mcp_third_party_app_removed CHECK ((removed_at IS NULL) = (removed_by IS NULL))
);
CREATE INDEX mcp_third_party_app_by_time ON fyniq.mcp_third_party_app (organisation_id, registered_at DESC);

-- A record keeps what it registered; only its removal is noted, once (FYM01); nothing is deleted.
-- +goose StatementBegin
CREATE FUNCTION fyniq.mcp_third_party_app_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A registered third-party AI app is kept: it is never deleted' USING ERRCODE = 'FYM01';
  END IF;
  IF (NEW.organisation_id, NEW.id, NEW.client_id, NEW.name, NEW.publisher, NEW.publisher_url, NEW.redirect_uris, NEW.purpose, NEW.processing_location, NEW.outside_singapore,
      NEW.inventory_entry, NEW.registered_by, NEW.registered_at)
     IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.client_id, OLD.name, OLD.publisher, OLD.publisher_url, OLD.redirect_uris, OLD.purpose, OLD.processing_location,
      OLD.outside_singapore, OLD.inventory_entry, OLD.registered_by, OLD.registered_at) THEN
    RAISE EXCEPTION 'What was registered never changes' USING ERRCODE = 'FYM01';
  END IF;
  IF OLD.removed_at IS NOT NULL THEN
    RAISE EXCEPTION 'A removed third-party AI app stays removed' USING ERRCODE = 'FYM01';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER mcp_third_party_app_guard BEFORE UPDATE OR DELETE ON fyniq.mcp_third_party_app FOR EACH ROW EXECUTE FUNCTION fyniq.mcp_third_party_app_guard();
CREATE TRIGGER mcp_third_party_app_no_truncate BEFORE TRUNCATE ON fyniq.mcp_third_party_app FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.mcp_third_party_app
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('mcp_third_party_app', 'id');

ALTER TABLE fyniq.mcp_third_party_app ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.mcp_third_party_app FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.mcp_third_party_app USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON FUNCTION fyniq.mcp_third_party_app_guard() FROM PUBLIC;
REVOKE ALL ON fyniq.mcp_third_party_app FROM PUBLIC;
GRANT SELECT ON fyniq.mcp_third_party_app TO fyniq_api, fyniq_readonly;
GRANT INSERT (id, client_id, name, publisher, publisher_url, redirect_uris, purpose, processing_location, outside_singapore, inventory_entry, registered_by, registered_at)
  ON fyniq.mcp_third_party_app TO fyniq_api;
GRANT UPDATE (removed_at, removed_by) ON fyniq.mcp_third_party_app TO fyniq_api;
