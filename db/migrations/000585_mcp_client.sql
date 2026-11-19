-- The AI apps that registered themselves (PRD-001 AI-90; p1-mcp-first-party task 11.1, design D14): the installation's own record
-- of each client the registration endpoint created at the OAuth server. A registration has no organisation until an Owner puts it
-- on their allowlist (mcp.clients), so the table is global (§11.4 "Global tables"): no organisation_id and no grant to any
-- application role. The services reach it only through the functions below, which are the policy's database half: the endpoint
-- opens only while some organisation has registration on (dual-controlled, off by default), one network may register only so
-- often, and only so many registrations may wait for approval. A registration is pending until an Owner approves it, may then be
-- approved or denied once, and expires when it is never approved or never used; nothing is ever deleted.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.mcp_client (
  client_id     text        NOT NULL,
  name          text        NOT NULL,
  redirect_uris text[]      NOT NULL,
  status        text        NOT NULL DEFAULT 'pending',
  network       text        NOT NULL DEFAULT '',          -- the registering address as a /24 or /48, never the address
  registered_at timestamptz NOT NULL,
  decided_at    timestamptz,
  last_used_at  timestamptz,
  CONSTRAINT mcp_client_pkey PRIMARY KEY (client_id),
  CONSTRAINT mcp_client_id CHECK (client_id ~ '^mcp-[0-9a-f]{20}$'),
  CONSTRAINT mcp_client_name CHECK (length(name) BETWEEN 1 AND 100),
  CONSTRAINT mcp_client_uris CHECK (cardinality(redirect_uris) BETWEEN 1 AND 5),
  CONSTRAINT mcp_client_status CHECK (status IN ('pending', 'approved', 'denied', 'expired') AND (status = 'pending') = (decided_at IS NULL)),
  CONSTRAINT mcp_client_network CHECK (length(network) <= 64)
);
CREATE INDEX mcp_client_pending ON fyniq.mcp_client (status, registered_at);
CREATE INDEX mcp_client_network_recent ON fyniq.mcp_client (network, registered_at);
REVOKE ALL ON fyniq.mcp_client FROM PUBLIC;

-- A registration goes only forward: pending to approved, denied or expired, approved to expired; nothing else changes but when it
-- was last used. Nothing is deleted.
-- +goose StatementBegin
CREATE FUNCTION fyniq.mcp_client_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A registered client is never deleted' USING ERRCODE = 'FY343';
  END IF;
  IF (NEW.client_id, NEW.name, NEW.redirect_uris, NEW.network, NEW.registered_at)
       IS DISTINCT FROM (OLD.client_id, OLD.name, OLD.redirect_uris, OLD.network, OLD.registered_at) THEN
    RAISE EXCEPTION 'A registered client''s name, redirects and origin never change' USING ERRCODE = 'FY343';
  END IF;
  IF NEW.status <> OLD.status AND NOT (OLD.status = 'pending' AND NEW.status IN ('approved', 'denied', 'expired')
                                       OR OLD.status = 'approved' AND NEW.status = 'expired') THEN
    RAISE EXCEPTION 'A registration goes from pending to approved, denied or expired, or from approved to expired' USING ERRCODE = 'FY343';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER mcp_client_guard BEFORE UPDATE OR DELETE ON fyniq.mcp_client
  FOR EACH ROW EXECUTE FUNCTION fyniq.mcp_client_guard();
CREATE TRIGGER mcp_client_no_truncate BEFORE TRUNCATE ON fyniq.mcp_client
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

-- Whether any organisation has registration on today: the setting mcp.registration, in force. The one read across tenants.
-- +goose StatementBegin
CREATE FUNCTION fyniq.mcp_registration_open(p_now timestamptz) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  o        uuid;
  v        text;
  previous text := current_setting('app.org_id', true);
BEGIN
  FOR o IN SELECT organisation_id FROM fyniq.tenant_directory LOOP
    PERFORM set_config('app.org_id', o::text, true);
    SELECT s.value INTO v FROM fyniq.organisation_setting s
     WHERE s.organisation_id = o AND s.key = 'mcp.registration' AND s.effective_from <= (p_now AT TIME ZONE 'UTC')::date
     ORDER BY s.effective_from DESC, s.version DESC LIMIT 1;
    IF v = 'true' THEN
      PERFORM set_config('app.org_id', coalesce(previous, ''), true);
      RETURN true;
    END IF;
  END LOOP;
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
  RETURN false;
END
$$;
-- +goose StatementEnd

-- Registers a client: '' when it was recorded, otherwise why not (limit_hour, limit_day, pending_full). The limits are counted
-- from the table, so every instance shares one count.
-- +goose StatementBegin
CREATE FUNCTION fyniq.mcp_client_register(p_id text, p_name text, p_uris text[], p_network text, p_now timestamptz,
  p_per_hour integer, p_per_day integer, p_pending_cap integer) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF (SELECT count(*) FROM fyniq.mcp_client WHERE network = p_network AND registered_at > p_now - interval '1 hour') >= p_per_hour THEN
    RETURN 'limit_hour';
  END IF;
  IF (SELECT count(*) FROM fyniq.mcp_client WHERE network = p_network AND registered_at > p_now - interval '1 day') >= p_per_day THEN
    RETURN 'limit_day';
  END IF;
  IF (SELECT count(*) FROM fyniq.mcp_client WHERE status = 'pending') >= p_pending_cap THEN
    RETURN 'pending_full';
  END IF;
  INSERT INTO fyniq.mcp_client (client_id, name, redirect_uris, network, registered_at) VALUES (p_id, p_name, p_uris, p_network, p_now);
  RETURN '';
END
$$;
-- +goose StatementEnd

-- A client's status; '' for one that is not registered.
-- +goose StatementBegin
CREATE FUNCTION fyniq.mcp_client_status(p_id text) RETURNS text
LANGUAGE sql SECURITY DEFINER STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$ SELECT coalesce((SELECT status FROM fyniq.mcp_client WHERE client_id = p_id), '') $$;
-- +goose StatementEnd

-- An Owner decides a pending registration, once: true when this call decided it.
-- +goose StatementBegin
CREATE FUNCTION fyniq.mcp_client_decide(p_id text, p_status text, p_now timestamptz) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE n integer;
BEGIN
  IF p_status NOT IN ('approved', 'denied') THEN
    RAISE EXCEPTION 'A registration is approved or denied' USING ERRCODE = 'FY343';
  END IF;
  UPDATE fyniq.mcp_client SET status = p_status, decided_at = p_now WHERE client_id = p_id AND status = 'pending';
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n = 1;
END
$$;
-- +goose StatementEnd

-- Notes that an approved client was used.
-- +goose StatementBegin
CREATE FUNCTION fyniq.mcp_client_touch(p_id text, p_now timestamptz) RETURNS void
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$ UPDATE fyniq.mcp_client SET last_used_at = greatest(coalesce(last_used_at, p_now), p_now) WHERE client_id = p_id AND status = 'approved' $$;
-- +goose StatementEnd

-- The registrations waiting for an Owner, newest first: what Connected apps lists to approve or deny.
-- +goose StatementBegin
CREATE FUNCTION fyniq.mcp_clients_pending() RETURNS SETOF fyniq.mcp_client
LANGUAGE sql SECURITY DEFINER STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$ SELECT * FROM fyniq.mcp_client c WHERE c.status = 'pending' ORDER BY c.registered_at DESC, c.client_id LIMIT 100 $$;
-- +goose StatementEnd

-- Expires what was never approved within p_pending, and what was approved and never used within p_unused; it returns their IDs,
-- for the caller to remove from the OAuth server.
-- +goose StatementBegin
CREATE FUNCTION fyniq.mcp_clients_expire(p_now timestamptz, p_pending interval, p_unused interval) RETURNS SETOF text
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  UPDATE fyniq.mcp_client SET status = 'expired', decided_at = p_now
   WHERE (status = 'pending' AND registered_at <= p_now - p_pending)
      OR (status = 'approved' AND coalesce(last_used_at, decided_at) <= p_now - p_unused)
  RETURNING client_id
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.mcp_registration_open(timestamptz), fyniq.mcp_client_register(text, text, text[], text, timestamptz, integer, integer, integer),
  fyniq.mcp_client_status(text), fyniq.mcp_client_decide(text, text, timestamptz), fyniq.mcp_client_touch(text, timestamptz),
  fyniq.mcp_clients_pending(), fyniq.mcp_clients_expire(timestamptz, interval, interval) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.mcp_registration_open(timestamptz), fyniq.mcp_client_register(text, text, text[], text, timestamptz, integer, integer, integer),
  fyniq.mcp_client_status(text), fyniq.mcp_client_decide(text, text, timestamptz), fyniq.mcp_client_touch(text, timestamptz),
  fyniq.mcp_clients_pending() TO fyniq_api;
-- The worker's scheduled run and the API (which tidies as registrations and approvals come in) may both expire what has lapsed.
GRANT EXECUTE ON FUNCTION fyniq.mcp_clients_expire(timestamptz, interval, interval) TO fyniq_api, fyniq_worker;
