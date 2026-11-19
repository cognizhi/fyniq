-- The handshake of the link on Nemura's side (PRD-002 NM-ARC-04, NM-INT-10, NM-INT-11; p1-nemura-fyniq-link tasks 1.1, 2.1, 2.3). The two apps hold each other's public
-- keys, and neither can be made first with the other's: Nemura proposes (its own keys, made now, and the ID its client at Nemura's own OAuth server will have), Fyniq links with those, and
-- Nemura activates with what Fyniq made (its envelope key and its client at Fyniq's OAuth server). So the link has a first state, proposed, in which Fyniq's key is not yet known.
--
-- Two keys, never one (p1-payroll-link design D1): the envelope key signs what Nemura sends; the client key signs the assertion that gets Nemura's token from Fyniq's OAuth server (private_key_jwt).
-- A stolen token alone cannot sign an event, a stolen envelope key alone has no token. Both private halves are the worker's to read; the API writes them once and cannot read them back.
-- Each widening of a CHECK is NOT VALID here and validated in the next migration.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.fyniq_link DROP CONSTRAINT fyniq_link_state_known;
ALTER TABLE nemura.fyniq_link ADD CONSTRAINT fyniq_link_state_known CHECK (state IN ('proposed', 'active', 'suspended', 'unlinking', 'unlinked')) NOT VALID;
ALTER TABLE nemura.fyniq_link DROP CONSTRAINT fyniq_link_keys;
-- squawk-ignore ban-drop-not-null
ALTER TABLE nemura.fyniq_link ALTER COLUMN fyniq_key DROP NOT NULL;   -- the table's rows are the link's own, a handful per organisation; the new check says when the key may be absent
ALTER TABLE nemura.fyniq_link ADD CONSTRAINT fyniq_link_keys CHECK (
  octet_length(link_public) = 32
  AND (fyniq_key IS NULL OR octet_length(fyniq_key) = 32)
  AND (state IN ('proposed', 'unlinked') OR fyniq_key IS NOT NULL)
  AND (state <> 'proposed' OR fyniq_key IS NULL)) NOT VALID;

-- The client Fyniq calls Nemura with: its ID at Nemura's own OAuth server is chosen when the link is proposed (Fyniq is told it, and registers nothing there); the client Nemura calls
-- Fyniq with is the one Fyniq's side of the link registered, and its ID is Fyniq's answer.
ALTER TABLE nemura.fyniq_link ADD COLUMN peer_client_id text;
ALTER TABLE nemura.fyniq_link ADD COLUMN fyniq_client_id text;
ALTER TABLE nemura.fyniq_link ADD COLUMN proposed_by uuid;
ALTER TABLE nemura.fyniq_link ADD CONSTRAINT fyniq_link_proposer_fkey FOREIGN KEY (organisation_id, proposed_by) REFERENCES nemura.member (organisation_id, id) NOT VALID;
ALTER TABLE nemura.fyniq_link ADD CONSTRAINT fyniq_link_clients_shape CHECK (
  (state = 'proposed' AND fyniq_client_id IS NULL)
  OR (state <> 'proposed' AND (fyniq_client_id IS NULL) = (peer_client_id IS NULL))) NOT VALID;
-- squawk-ignore require-concurrent-index-creation
CREATE UNIQUE INDEX fyniq_link_peer_client ON nemura.fyniq_link (peer_client_id) WHERE peer_client_id IS NOT NULL;

ALTER TABLE nemura.fyniq_link_secret ADD COLUMN client_private bytea;   -- Nemura's ES256 key for the assertion at Fyniq's token endpoint (PKCS #8, DER)
ALTER TABLE nemura.fyniq_link_secret ADD COLUMN client_public jsonb;    -- its public half as a JWK, which Fyniq registers; never secret
ALTER TABLE nemura.fyniq_link_secret ADD CONSTRAINT fyniq_link_secret_client_size CHECK (client_private IS NULL OR octet_length(client_private) BETWEEN 32 AND 400) NOT VALID;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.fyniq_link_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  top integer;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A link is kept: unlink it instead' USING ERRCODE = 'NM190';
  END IF;
  IF TG_OP = 'INSERT' THEN
    SELECT coalesce(max(epoch), 0) INTO top FROM nemura.fyniq_link WHERE organisation_id = NEW.organisation_id;
    IF NEW.state NOT IN ('proposed', 'active') OR NEW.version <> 1 OR NEW.epoch <> top + 1 THEN
      RAISE EXCEPTION 'A link starts proposed or active, at its first version, one epoch above the last' USING ERRCODE = 'NM190';
    END IF;
    RETURN NEW;
  END IF;
  -- A proposed link is completed once: Fyniq's key and its client are written by the move to active, and nothing else of it changes then or after.
  IF (NEW.id, NEW.epoch, NEW.fyniq_organisation_id, NEW.link_public, NEW.created_at, NEW.peer_client_id, NEW.proposed_by) IS DISTINCT FROM
     (OLD.id, OLD.epoch, OLD.fyniq_organisation_id, OLD.link_public, OLD.created_at, OLD.peer_client_id, OLD.proposed_by)
     OR ((NEW.fyniq_key, NEW.fyniq_client_id) IS DISTINCT FROM (OLD.fyniq_key, OLD.fyniq_client_id) AND NOT (OLD.state = 'proposed' AND NEW.state = 'active'))
     OR NEW.sequence < OLD.sequence
     OR NEW.version <> OLD.version + 1
     OR (NEW.state <> OLD.state AND NOT ((OLD.state, NEW.state) IN (('proposed', 'active'), ('proposed', 'unlinked'), ('active', 'suspended'), ('suspended', 'active'),
                                       ('active', 'unlinking'), ('suspended', 'unlinking'), ('unlinking', 'unlinked'), ('unlinking', 'active')))) THEN
    RAISE EXCEPTION 'A link only moves between its states, at its next version, and keeps its epoch and keys' USING ERRCODE = 'NM190';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- Which link a bearer token's client belongs to, read before any organisation is set (the token names a client, not an organisation): a global route from the client's ID to the organisation and
-- the link, written with the link and read through one function, as the identity directory is. It names nothing else; the caller then works inside that organisation, where row-level security
-- applies, and reads the link's state and epoch there.
CREATE TABLE nemura.link_client_route (
  peer_client_id  text NOT NULL,
  organisation_id uuid NOT NULL,
  link_id         uuid NOT NULL,
  CONSTRAINT link_client_route_pkey PRIMARY KEY (peer_client_id)
);
REVOKE ALL ON nemura.link_client_route FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION nemura.link_client_registered() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  INSERT INTO nemura.link_client_route (peer_client_id, organisation_id, link_id) VALUES (NEW.peer_client_id, NEW.organisation_id, NEW.id);
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER link_client_registered AFTER INSERT ON nemura.fyniq_link
  FOR EACH ROW WHEN (NEW.peer_client_id IS NOT NULL) EXECUTE FUNCTION nemura.link_client_registered();

-- +goose StatementBegin
CREATE FUNCTION nemura.link_client_route(p_client text, OUT organisation_id uuid, OUT link_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT r.organisation_id, r.link_id FROM nemura.link_client_route r WHERE r.peer_client_id = p_client
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION nemura.link_client_route(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.link_client_route(text) TO nemura_api;

-- The API proposes and activates a link; the worker signs with the keys, and the API cannot read them back.
GRANT INSERT (peer_client_id, proposed_by) ON nemura.fyniq_link TO nemura_api;
GRANT UPDATE (fyniq_key, fyniq_client_id) ON nemura.fyniq_link TO nemura_api;
GRANT INSERT (client_private, client_public) ON nemura.fyniq_link_secret TO nemura_api;
