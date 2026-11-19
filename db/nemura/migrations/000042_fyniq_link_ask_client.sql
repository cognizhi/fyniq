-- The synchronous ask at release (PRD-001 FR-PPL-13; p1-payroll-link task 6.3, design D10; decided 20 Nov 2026, owner to confirm): when Fyniq releases a payment to a person, it asks Nemura, the hold's
-- authority, whether the person is held now, and pays no one it cannot get an answer for within five seconds. The API makes that call, and the API must never hold the key that signs events or the
-- client that sends them, so the ask has a client of its own: a second client Fyniq holds at Nemura's OAuth server, with one scope, link.holds.read, which reaches one read of a hold's status and nothing
-- that writes. Nemura chooses its ID when it proposes the link, as for the events' client, and registers it when the link is approved, with the public key Fyniq gives.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.fyniq_link ADD COLUMN peer_ask_client_id text;
-- squawk-ignore require-concurrent-index-creation
CREATE UNIQUE INDEX fyniq_link_peer_ask_client ON nemura.fyniq_link (peer_ask_client_id) WHERE peer_ask_client_id IS NOT NULL;
ALTER TABLE nemura.fyniq_link ADD CONSTRAINT fyniq_link_ask_differs CHECK (peer_ask_client_id IS NULL OR peer_ask_client_id <> peer_client_id) NOT VALID;

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
  IF (NEW.id, NEW.epoch, NEW.fyniq_organisation_id, NEW.link_public, NEW.created_at, NEW.peer_client_id, NEW.peer_ask_client_id, NEW.proposed_by) IS DISTINCT FROM
     (OLD.id, OLD.epoch, OLD.fyniq_organisation_id, OLD.link_public, OLD.created_at, OLD.peer_client_id, OLD.peer_ask_client_id, OLD.proposed_by)
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

-- The route from a client to its link now says which of the link's two clients it is.
ALTER TABLE nemura.link_client_route ADD COLUMN purpose text NOT NULL DEFAULT 'events';
ALTER TABLE nemura.link_client_route ADD CONSTRAINT link_client_route_purpose CHECK (purpose IN ('events', 'ask')) NOT VALID;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.link_client_registered() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF NEW.peer_client_id IS NOT NULL THEN
    INSERT INTO nemura.link_client_route (peer_client_id, organisation_id, link_id, purpose) VALUES (NEW.peer_client_id, NEW.organisation_id, NEW.id, 'events');
  END IF;
  IF NEW.peer_ask_client_id IS NOT NULL THEN
    INSERT INTO nemura.link_client_route (peer_client_id, organisation_id, link_id, purpose) VALUES (NEW.peer_ask_client_id, NEW.organisation_id, NEW.id, 'ask');
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
DROP TRIGGER link_client_registered ON nemura.fyniq_link;
CREATE TRIGGER link_client_registered AFTER INSERT ON nemura.fyniq_link FOR EACH ROW EXECUTE FUNCTION nemura.link_client_registered();

DROP FUNCTION nemura.link_client_route(text);
-- +goose StatementBegin
CREATE FUNCTION nemura.link_client_route(p_client text, OUT organisation_id uuid, OUT link_id uuid, OUT purpose text)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT r.organisation_id, r.link_id, r.purpose FROM nemura.link_client_route r WHERE r.peer_client_id = p_client
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION nemura.link_client_route(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.link_client_route(text) TO nemura_api;

GRANT INSERT (peer_ask_client_id) ON nemura.fyniq_link TO nemura_api;
