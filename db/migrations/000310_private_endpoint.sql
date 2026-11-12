-- Registered private endpoints (PRD-001 SEC-37, rev 0.16; p1-security-hardening task 5.1, design D3): a destination on a
-- private network (a self-hosted model, AI-79, or a connector inside an organisation's network) is reachable only once a
-- Super admin, the platform operator in the app, registers it: its private address or range, its port, the SHA-256 of
-- the key its TLS certificate must present, the service and the organisation it is for, and the credential bound to it.
-- Each change is a new version, a retirement too; no version is changed or removed (FY321), and a retired endpoint is
-- final. The table is global: no application role holds a grant on it. The API writes it only through
-- register_private_endpoint, in a unit of work of the installation organisation whose trail records each version, and
-- with two Super admins only as the other's approved installation change of step private_endpoint (FR-ADM-07, FY213).
-- The connector runtime reads its own organisation's, for one credential, through private_endpoints_for.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
-- Whether an address may be registered: a private range (10/8, 172.16/12, 192.168/16, 100.64/10 or fc00::/7), no wider
-- than a /24 or a /64.
CREATE FUNCTION fyniq.private_endpoint_address_ok(p_address cidr) RETURNS boolean
LANGUAGE sql IMMUTABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT (p_address <<= '10.0.0.0/8' OR p_address <<= '172.16.0.0/12' OR p_address <<= '192.168.0.0/16'
          OR p_address <<= '100.64.0.0/10' OR p_address <<= 'fc00::/7')
     AND masklen(p_address) >= CASE WHEN family(p_address) = 4 THEN 24 ELSE 64 END
$$;
-- +goose StatementEnd

CREATE TABLE fyniq.private_endpoint (
  id          uuid        NOT NULL,
  version     integer     NOT NULL,
  address     cidr        NOT NULL,
  port        integer     NOT NULL,
  tls_pin     text        NOT NULL,
  service     text        NOT NULL,
  serves      uuid        NOT NULL,
  credential  uuid        NOT NULL,
  retired     boolean     NOT NULL DEFAULT false,
  reason      text        NOT NULL,
  recorded_by uuid        NOT NULL,
  recorded_at timestamptz NOT NULL,
  change_id   uuid,
  CONSTRAINT private_endpoint_pkey PRIMARY KEY (id, version),
  CONSTRAINT private_endpoint_recorded_by_fkey FOREIGN KEY (recorded_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT private_endpoint_change_fkey FOREIGN KEY (change_id) REFERENCES fyniq.installation_change (id),
  CONSTRAINT private_endpoint_version_positive CHECK (version >= 1),
  CONSTRAINT private_endpoint_port_range CHECK (port BETWEEN 1 AND 65535),
  CONSTRAINT private_endpoint_pin_format CHECK (tls_pin ~ '^[A-Za-z0-9+/]{43}=$'),
  CONSTRAINT private_endpoint_service_known CHECK (service IN ('connector', 'ai_model')),
  CONSTRAINT private_endpoint_reason_given CHECK (length(btrim(reason)) BETWEEN 3 AND 500),
  -- A private range, no wider than a /24 (IPv4) or a /64 (IPv6): never loopback, link-local or the metadata service.
  CONSTRAINT private_endpoint_private_address CHECK (fyniq.private_endpoint_address_ok(address))
);
CREATE INDEX private_endpoint_serves ON fyniq.private_endpoint (serves, credential);
REVOKE ALL ON fyniq.private_endpoint FROM PUBLIC;

ALTER TABLE fyniq.installation_change DROP CONSTRAINT installation_change_step_known;
ALTER TABLE fyniq.installation_change ADD CONSTRAINT installation_change_step_known
  CHECK (step IN ('install', 'configure', 'secret', 'enable', 'resume', 'disable', 'uninstall', 'order', 'consent', 'sign_in',
    'private_endpoint')) NOT VALID;

-- +goose StatementBegin
-- Versions are only added (FY321): a change is a new version, never an edit, and nothing is removed.
CREATE FUNCTION fyniq.private_endpoint_append_only() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A private endpoint''s version is never changed or removed' USING ERRCODE = 'FY321',
    HINT = 'Register a new version of the endpoint, or retire it.';
END
$$;
-- +goose StatementEnd

CREATE TRIGGER private_endpoint_append_only BEFORE UPDATE OR DELETE ON fyniq.private_endpoint
  FOR EACH ROW EXECUTE FUNCTION fyniq.private_endpoint_append_only();

-- +goose StatementBegin
-- The current version of every endpoint, retired ones included.
CREATE FUNCTION fyniq.private_endpoints() RETURNS SETOF fyniq.private_endpoint
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT DISTINCT ON (e.id) e.* FROM fyniq.private_endpoint e ORDER BY e.id, e.version DESC
$$;

-- The endpoints a credential of the caller's organisation may reach: their current versions, not retired, registered for
-- this organisation, service and credential, and no other organisation's (the connector runtime, SEC-37).
CREATE FUNCTION fyniq.private_endpoints_for(p_service text, p_credential uuid) RETURNS SETOF fyniq.private_endpoint
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT c.* FROM fyniq.private_endpoints() c
   WHERE NOT c.retired AND c.serves = fyniq.current_org() AND c.service = p_service AND c.credential = p_credential
   ORDER BY c.address, c.port
$$;

-- An active Super admin registers version p_version + 1 of endpoint p_id (p_version 0: a new one), as approved by
-- p_change when two Super admins must agree (FY213). Refused at another version (FY320), for a retired endpoint, an
-- address that is not a private range of at most /24 or /64, a pin that is not one, or an organisation that does not
-- exist (FY319). Returns the new version's number.
CREATE FUNCTION fyniq.register_private_endpoint(p_by uuid, p_change uuid, p_id uuid, p_version integer, p_address text, p_port integer,
  p_pin text, p_service text, p_serves uuid, p_credential uuid, p_retired boolean, p_reason text, p_at timestamptz) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  cur  fyniq.private_endpoint;
  addr cidr;
BEGIN
  PERFORM fyniq.require_super_admin(p_by);
  PERFORM fyniq.apply_installation_change(p_change, 'private-endpoint:' || p_id, NULL, 'private_endpoint', p_at);
  BEGIN
    addr := p_address::cidr;
  EXCEPTION WHEN invalid_text_representation OR invalid_binary_representation THEN
    RAISE EXCEPTION 'Address % is not an address or a range', p_address USING ERRCODE = 'FY319',
      HINT = 'Give the private address, such as 10.20.0.5, or its range, such as 10.20.0.0/28.';
  END;
  IF NOT fyniq.private_endpoint_address_ok(addr) THEN
    RAISE EXCEPTION 'Address % is not a private range of at most /24 (IPv4) or /64 (IPv6)', p_address USING ERRCODE = 'FY319',
      HINT = 'Register the private address or the narrowest range the service listens on; a public address needs no registration.';
  END IF;
  IF p_pin IS NULL OR p_pin !~ '^[A-Za-z0-9+/]{43}=$' THEN
    RAISE EXCEPTION 'The pin is not the base64 of a SHA-256' USING ERRCODE = 'FY319',
      HINT = 'Give the base64 SHA-256 of the certificate''s SubjectPublicKeyInfo: 44 characters ending in =.';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.tenant_directory t WHERE t.organisation_id = p_serves) THEN
    RAISE EXCEPTION 'Organisation % does not exist', p_serves USING ERRCODE = 'FY319', HINT = 'Name the organisation the endpoint is for.';
  END IF;
  SELECT * INTO cur FROM fyniq.private_endpoint e WHERE e.id = p_id ORDER BY e.version DESC LIMIT 1 FOR UPDATE;
  IF coalesce(cur.version, 0) <> p_version THEN
    RAISE EXCEPTION 'Private endpoint % changed since version %', p_id, p_version USING ERRCODE = 'FY320',
      HINT = 'Read the endpoint again and make the change on what it is now.';
  END IF;
  IF cur.retired THEN
    RAISE EXCEPTION 'Private endpoint % is retired', p_id USING ERRCODE = 'FY319', HINT = 'Register a new endpoint instead.';
  END IF;
  INSERT INTO fyniq.private_endpoint (id, version, address, port, tls_pin, service, serves, credential, retired, reason, recorded_by,
    recorded_at, change_id)
  VALUES (p_id, p_version + 1, addr, p_port, p_pin, p_service, p_serves, p_credential, coalesce(p_retired, false), p_reason, p_by, p_at,
    p_change);
  RETURN p_version + 1;
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.private_endpoints(), fyniq.private_endpoints_for(text, uuid), fyniq.private_endpoint_address_ok(cidr),
  fyniq.register_private_endpoint(uuid, uuid, uuid, integer, text, integer, text, text, uuid, uuid, boolean, text, timestamptz),
  fyniq.private_endpoint_append_only() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.private_endpoints(),
  fyniq.register_private_endpoint(uuid, uuid, uuid, integer, text, integer, text, text, uuid, uuid, boolean, text, timestamptz) TO fyniq_api;
GRANT EXECUTE ON FUNCTION fyniq.private_endpoints_for(text, uuid) TO fyniq_api, fyniq_worker;
