-- Connectors (PRD-001 FR-INT-20, FR-INT-25…27, rev 0.139; p1-connector-framework task 4.1): an outside service
-- installed from the release's catalogue, installation-wide by a Super admin or for an organisation by its Owner or
-- Administrator. Each installed connector keeps the manifest it consented to (version and SHA-256), its state in the
-- lifecycle (installed → configured → tested → enabled, paused, disabled; uninstalled for good), its non-secret
-- configuration, and its position among the connectors of the same kind (the email failover order). A secret is never
-- in the configuration: the row keeps the manifest's secret names and refuses a configuration that holds one. Secrets
-- are references to Secret Manager versions, one row per version, replaced by a new row and kept valid for the
-- manifest's overlap, then destroyed.
--
-- The installation's tables are global and written only through SECURITY DEFINER functions that check an active
-- Super admin (FY085) and, once there are two, an approved installation change for every step but pause (FY213): the
-- second Super admin's dual control (FR-ADM-07; the owner's decision of 25 Oct 2026 for pause). The first-start
-- bootstrap installs the email connectors from the deployment's settings only into an empty table (FY214). An
-- organisation's are tenant tables under FORCE RLS, audited in the same transaction, changed through the approval
-- engine (object type connector, FM-1).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- ------------------------------------------------------------------ the installation's connectors
CREATE TABLE fyniq.installation_connector (
  id               uuid        NOT NULL DEFAULT uuidv7(),
  connector        text        NOT NULL,  -- the manifest's id
  manifest_version text        NOT NULL,  -- the manifest consented to
  manifest_hash    text        NOT NULL,  -- its SHA-256 (RFC 8785 form), hex
  secret_names     text[]      NOT NULL,  -- the consented manifest's secrets, which the configuration never holds
  consent          jsonb       NOT NULL DEFAULT '{}',  -- what it consented to send and reach: data classes, scopes, destinations, webhooks
  state            text        NOT NULL,
  configuration    jsonb       NOT NULL DEFAULT '{}',
  position         integer     NOT NULL,  -- among the connectors of its kind: 1 is the primary
  test_passed      boolean,
  test_reason      text,                  -- the reason Fyniq wrote, never the provider's answer (SEC-36)
  tested_at        timestamptz,
  installed_by     text        NOT NULL,  -- user:<id>, or service:api for the bootstrap
  installed_at     timestamptz NOT NULL,
  changed_by       text        NOT NULL,
  changed_at       timestamptz NOT NULL,
  version          integer     NOT NULL DEFAULT 1,
  CONSTRAINT installation_connector_pkey PRIMARY KEY (id),
  CONSTRAINT installation_connector_id_form CHECK (connector ~ '^[a-z][a-z0-9-]{1,39}$'),
  CONSTRAINT installation_connector_version_form CHECK (manifest_version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'),
  CONSTRAINT installation_connector_hash_form CHECK (manifest_hash ~ '^[0-9a-f]{64}$'),
  CONSTRAINT installation_connector_secret_names_form CHECK (cardinality(secret_names) <= 20
    AND array_to_string(secret_names, ',') ~ '^([a-z][a-z0-9_]{1,39}(,[a-z][a-z0-9_]{1,39})*)?$'),
  CONSTRAINT installation_connector_state_known CHECK (state IN ('installed', 'configured', 'tested', 'enabled', 'paused', 'disabled', 'uninstalled')),
  CONSTRAINT installation_connector_consent_object CHECK (jsonb_typeof(consent) = 'object' AND length(consent::text) <= 16384),
  CONSTRAINT installation_connector_configuration_object CHECK (jsonb_typeof(configuration) = 'object' AND length(configuration::text) <= 65536),
  CONSTRAINT installation_connector_no_secret CHECK (NOT configuration ?| secret_names),
  CONSTRAINT installation_connector_position_positive CHECK (position BETWEEN 1 AND 1000),
  CONSTRAINT installation_connector_test_whole CHECK ((test_passed IS NULL) = (tested_at IS NULL) AND (test_reason IS NULL OR test_passed IS NOT NULL)),
  CONSTRAINT installation_connector_test_reason_length CHECK (length(test_reason) <= 300),
  CONSTRAINT installation_connector_by_form CHECK (installed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND changed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT installation_connector_version_positive CHECK (version >= 1)
);
-- One live installation per connector.
CREATE UNIQUE INDEX installation_connector_live ON fyniq.installation_connector (connector) WHERE state <> 'uninstalled';
REVOKE ALL ON fyniq.installation_connector FROM PUBLIC;

CREATE TABLE fyniq.installation_connector_secret (
  id           uuid        NOT NULL DEFAULT uuidv7(),
  connector_id uuid        NOT NULL,
  name         text        NOT NULL,
  ref          text        NOT NULL,  -- the Secret Manager version (ports.SecretRef); never the value
  set_by       text        NOT NULL,
  set_at       timestamptz NOT NULL,
  valid_until  timestamptz,           -- set when replaced: the end of the manifest's overlap
  destroyed_at timestamptz,
  CONSTRAINT installation_connector_secret_pkey PRIMARY KEY (id),
  CONSTRAINT installation_connector_secret_connector_fkey FOREIGN KEY (connector_id) REFERENCES fyniq.installation_connector (id),
  CONSTRAINT installation_connector_secret_name_form CHECK (name ~ '^[a-z][a-z0-9_]{1,39}$'),
  CONSTRAINT installation_connector_secret_ref_form CHECK (length(ref) BETWEEN 3 AND 512 AND ref ~ '^[A-Za-z0-9:/_.-]+$'),
  CONSTRAINT installation_connector_secret_by_form CHECK (set_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT installation_connector_secret_once UNIQUE (ref)
);
-- At most one current version of each secret of a connector.
CREATE UNIQUE INDEX installation_connector_secret_current ON fyniq.installation_connector_secret (connector_id, name)
  WHERE valid_until IS NULL AND destroyed_at IS NULL;
REVOKE ALL ON fyniq.installation_connector_secret FROM PUBLIC;

-- A change to an installation connector waiting for the other Super admin (FR-ADM-07, FR-INT-26): the change and the
-- RFC 8785 envelope its decider signs, with the envelope's SHA-256, which the database recomputes; decided once, by
-- another active Super admin, before it expires; applied once.
CREATE TABLE fyniq.installation_change (
  id               uuid        NOT NULL DEFAULT uuidv7(),
  connector_id     uuid,                  -- none for an install
  connector        text        NOT NULL,
  step             text        NOT NULL,
  summary          text        NOT NULL,
  change           jsonb       NOT NULL,
  envelope         text        NOT NULL,
  envelope_hash    bytea       NOT NULL,
  requested_by     uuid        NOT NULL,
  requested_at     timestamptz NOT NULL,
  requester_mfa_at timestamptz NOT NULL,
  expires_at       timestamptz NOT NULL,
  status           text        NOT NULL DEFAULT 'pending',
  decided_by       uuid,
  decided_at       timestamptz,
  decider_mfa_at   timestamptz,
  reason           text,
  applied_at       timestamptz,
  CONSTRAINT installation_change_pkey PRIMARY KEY (id),
  CONSTRAINT installation_change_connector_fkey FOREIGN KEY (connector_id) REFERENCES fyniq.installation_connector (id),
  CONSTRAINT installation_change_requested_by_fkey FOREIGN KEY (requested_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT installation_change_decided_by_fkey FOREIGN KEY (decided_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT installation_change_step_known CHECK (step IN ('install', 'configure', 'secret', 'enable', 'resume', 'disable', 'uninstall', 'order', 'consent')),
  CONSTRAINT installation_change_status_known CHECK (status IN ('pending', 'approved', 'rejected', 'expired')),
  CONSTRAINT installation_change_summary_length CHECK (length(summary) BETWEEN 1 AND 300),
  CONSTRAINT installation_change_object CHECK (jsonb_typeof(change) = 'object' AND length(envelope) <= 65536),
  CONSTRAINT installation_change_hash_length CHECK (length(envelope_hash) = 32),
  CONSTRAINT installation_change_decided_whole CHECK ((status IN ('pending', 'expired')) = (decided_by IS NULL)
    AND (decided_by IS NULL) = (decided_at IS NULL) AND (decided_by IS NULL) = (decider_mfa_at IS NULL)),
  CONSTRAINT installation_change_another_person CHECK (decided_by <> requested_by),
  CONSTRAINT installation_change_applied_approved CHECK (applied_at IS NULL OR status = 'approved'),
  CONSTRAINT installation_change_reason_length CHECK (length(reason) <= 500),
  CONSTRAINT installation_change_expiry CHECK (expires_at > requested_at)
);
CREATE INDEX installation_change_pending ON fyniq.installation_change (connector) WHERE status = 'pending';
REVOKE ALL ON fyniq.installation_change FROM PUBLIC;

-- ------------------------------------------------------------------ an organisation's connectors
CREATE TABLE fyniq.organisation_connector (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  connector        text        NOT NULL,
  manifest_version text        NOT NULL,
  manifest_hash    text        NOT NULL,
  secret_names     text[]      NOT NULL,
  consent          jsonb       NOT NULL DEFAULT '{}',
  state            text        NOT NULL,
  configuration    jsonb       NOT NULL DEFAULT '{}',
  position         integer     NOT NULL,
  test_passed      boolean,
  test_reason      text,
  tested_at        timestamptz,
  installed_by     text        NOT NULL,
  installed_at     timestamptz NOT NULL,
  changed_by       text        NOT NULL,
  changed_at       timestamptz NOT NULL,
  version          integer     NOT NULL DEFAULT 1,
  CONSTRAINT organisation_connector_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT organisation_connector_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT organisation_connector_id_form CHECK (connector ~ '^[a-z][a-z0-9-]{1,39}$'),
  CONSTRAINT organisation_connector_version_form CHECK (manifest_version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'),
  CONSTRAINT organisation_connector_hash_form CHECK (manifest_hash ~ '^[0-9a-f]{64}$'),
  CONSTRAINT organisation_connector_secret_names_form CHECK (cardinality(secret_names) <= 20
    AND array_to_string(secret_names, ',') ~ '^([a-z][a-z0-9_]{1,39}(,[a-z][a-z0-9_]{1,39})*)?$'),
  CONSTRAINT organisation_connector_state_known CHECK (state IN ('installed', 'configured', 'tested', 'enabled', 'paused', 'disabled', 'uninstalled')),
  CONSTRAINT organisation_connector_consent_object CHECK (jsonb_typeof(consent) = 'object' AND length(consent::text) <= 16384),
  CONSTRAINT organisation_connector_configuration_object CHECK (jsonb_typeof(configuration) = 'object' AND length(configuration::text) <= 65536),
  CONSTRAINT organisation_connector_no_secret CHECK (NOT configuration ?| secret_names),
  CONSTRAINT organisation_connector_position_positive CHECK (position BETWEEN 1 AND 1000),
  CONSTRAINT organisation_connector_test_whole CHECK ((test_passed IS NULL) = (tested_at IS NULL) AND (test_reason IS NULL OR test_passed IS NOT NULL)),
  CONSTRAINT organisation_connector_test_reason_length CHECK (length(test_reason) <= 300),
  CONSTRAINT organisation_connector_by_form CHECK (installed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND changed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT organisation_connector_version_positive CHECK (version >= 1)
);
CREATE UNIQUE INDEX organisation_connector_live ON fyniq.organisation_connector (organisation_id, connector) WHERE state <> 'uninstalled';

CREATE TABLE fyniq.organisation_connector_secret (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  connector_id    uuid        NOT NULL,
  name            text        NOT NULL,
  ref             text        NOT NULL,
  set_by          text        NOT NULL,
  set_at          timestamptz NOT NULL,
  valid_until     timestamptz,
  destroyed_at    timestamptz,
  CONSTRAINT organisation_connector_secret_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT organisation_connector_secret_connector_fkey FOREIGN KEY (organisation_id, connector_id)
    REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT organisation_connector_secret_name_form CHECK (name ~ '^[a-z][a-z0-9_]{1,39}$'),
  CONSTRAINT organisation_connector_secret_ref_form CHECK (length(ref) BETWEEN 3 AND 512 AND ref ~ '^[A-Za-z0-9:/_.-]+$'),
  CONSTRAINT organisation_connector_secret_by_form CHECK (set_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT organisation_connector_secret_once UNIQUE (organisation_id, ref)
);
CREATE UNIQUE INDEX organisation_connector_secret_current ON fyniq.organisation_connector_secret (organisation_id, connector_id, name)
  WHERE valid_until IS NULL AND destroyed_at IS NULL;

-- ------------------------------------------------------------------ the lifecycle, for both
-- +goose StatementBegin
-- A connector moves only along the lifecycle (FY207); its identity and installation never change (FY205), nor is it
-- deleted; an uninstalled connector is final (FY206); its version rises by one with each change.
CREATE FUNCTION fyniq.connector_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A connector is never deleted' USING ERRCODE = 'FY205', HINT = 'Uninstall it: its history stays (FR-INT-26).';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NOT (NEW.state = 'installed' OR (NEW.state = 'enabled' AND TG_TABLE_NAME = 'installation_connector')) OR NEW.version <> 1 THEN
      RAISE EXCEPTION 'A connector is installed, or enabled by the first-start bootstrap' USING ERRCODE = 'FY207';
    END IF;
    RETURN NEW;
  END IF;
  IF OLD.state = 'uninstalled' THEN
    RAISE EXCEPTION 'Connector % is uninstalled', OLD.connector USING ERRCODE = 'FY206', HINT = 'Install it again (FR-INT-26).';
  END IF;
  IF (NEW.id, NEW.connector, NEW.installed_by, NEW.installed_at) IS DISTINCT FROM (OLD.id, OLD.connector, OLD.installed_by, OLD.installed_at)
     OR NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'A connector''s identity and installation never change, and its version rises by one' USING ERRCODE = 'FY205';
  END IF;
  IF NEW.state <> OLD.state AND NOT ((OLD.state, NEW.state) IN (
       ('installed', 'configured'), ('configured', 'tested'), ('tested', 'configured'), ('tested', 'enabled'),
       ('enabled', 'paused'), ('paused', 'enabled'), ('enabled', 'disabled'), ('paused', 'disabled'),
       ('disabled', 'configured'), ('disabled', 'tested'), ('disabled', 'enabled'),
       ('installed', 'uninstalled'), ('configured', 'uninstalled'), ('tested', 'uninstalled'), ('enabled', 'uninstalled'),
       ('paused', 'uninstalled'), ('disabled', 'uninstalled'))) THEN
    RAISE EXCEPTION 'Connector % cannot go from % to %', OLD.connector, OLD.state, NEW.state USING ERRCODE = 'FY207',
      HINT = 'install → configure → test → enable; pause and resume; disable; uninstall (FR-INT-26).';
  END IF;
  RETURN NEW;
END
$$;

-- A secret version is never deleted or changed, but is marked replaced (its overlap's end) and destroyed, each once
-- (FY208); its name is one of its connector's (FY208).
CREATE FUNCTION fyniq.connector_secret_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     OR (TG_OP = 'UPDATE' AND ((NEW.id, NEW.connector_id, NEW.name, NEW.ref, NEW.set_by, NEW.set_at)
            IS DISTINCT FROM (OLD.id, OLD.connector_id, OLD.name, OLD.ref, OLD.set_by, OLD.set_at)
         OR (OLD.valid_until IS NOT NULL AND NEW.valid_until IS DISTINCT FROM OLD.valid_until)
         OR (OLD.destroyed_at IS NOT NULL AND NEW.destroyed_at IS DISTINCT FROM OLD.destroyed_at))) THEN
    RAISE EXCEPTION 'A connector''s secret version is only replaced and destroyed, each once' USING ERRCODE = 'FY208',
      HINT = 'Set a new version instead (FR-INT-26).';
  END IF;
  RETURN NEW;
END
$$;

-- Reads the connector's table, global or the organisation's, so it runs as its owner.
CREATE FUNCTION fyniq.connector_secret_named() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  named boolean;
BEGIN
  IF TG_TABLE_NAME = 'installation_connector_secret' THEN
    named := EXISTS (SELECT 1 FROM fyniq.installation_connector c
                      WHERE c.id = NEW.connector_id AND NEW.name = ANY (c.secret_names) AND c.state <> 'uninstalled');
  ELSE
    EXECUTE 'SELECT EXISTS (SELECT 1 FROM fyniq.organisation_connector c WHERE c.organisation_id = $1::uuid AND c.id = $2 '
            'AND $3 = ANY (c.secret_names) AND c.state <> ''uninstalled'')'
      INTO named USING to_jsonb(NEW)->>'organisation_id', NEW.connector_id, NEW.name;
  END IF;
  IF NOT named THEN
    RAISE EXCEPTION 'Secret % is not one of its connector''s, or the connector is uninstalled', NEW.name USING ERRCODE = 'FY208';
  END IF;
  RETURN NEW;
END
$$;

-- An installation change is decided once, by another active Super admin, before it expires, and applied once; nothing
-- else about it changes (FY209).
CREATE FUNCTION fyniq.installation_change_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.envelope_hash <> sha256(convert_to(NEW.envelope, 'UTF8')) OR NEW.status <> 'pending' THEN
      RAISE EXCEPTION 'The envelope''s hash is not its SHA-256, or the change is not pending' USING ERRCODE = 'FY209';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'DELETE'
     OR (NEW.id, NEW.connector_id, NEW.connector, NEW.step, NEW.summary, NEW.change, NEW.envelope, NEW.envelope_hash, NEW.requested_by,
         NEW.requested_at, NEW.requester_mfa_at, NEW.expires_at)
        IS DISTINCT FROM (OLD.id, OLD.connector_id, OLD.connector, OLD.step, OLD.summary, OLD.change, OLD.envelope, OLD.envelope_hash,
                          OLD.requested_by, OLD.requested_at, OLD.requester_mfa_at, OLD.expires_at)
     OR (OLD.status <> 'pending' AND NEW.status IS DISTINCT FROM OLD.status)
     OR (OLD.applied_at IS NOT NULL AND NEW.applied_at IS DISTINCT FROM OLD.applied_at) THEN
    RAISE EXCEPTION 'An installation change is decided once and applied once' USING ERRCODE = 'FY209';
  END IF;
  RETURN NEW;
END
$$;

-- Whether a step on an installation connector needs the other Super admin: every step but pause, once there are two
-- active Super admins (FR-ADM-07; the owner's decision of 25 Oct 2026).
CREATE FUNCTION fyniq.installation_dual_control() RETURNS boolean
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT count(*) >= 2 FROM fyniq.super_admin s JOIN fyniq.app_user u USING (user_id) WHERE s.removed_at IS NULL AND u.status = 'active'
$$;

-- The approved, unapplied change p_change for connector p_connector (p_id for an installed one) and step p_step, which
-- it marks applied; none needed with one Super admin, or for a pause (FY213).
CREATE FUNCTION fyniq.apply_installation_change(p_change uuid, p_connector text, p_id uuid, p_step text, p_at timestamptz) RETURNS void
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF p_change IS NULL THEN
    IF fyniq.installation_dual_control() AND p_step <> 'pause' THEN
      RAISE EXCEPTION 'This step on connector % needs the other Super admin''s approval', p_connector USING ERRCODE = 'FY213',
        HINT = 'Request the change; the other Super admin approves it (FR-ADM-07, FR-INT-26).';
    END IF;
    RETURN;
  END IF;
  UPDATE fyniq.installation_change SET applied_at = p_at
   WHERE id = p_change AND status = 'approved' AND applied_at IS NULL AND connector = p_connector AND step = p_step
     AND connector_id IS NOT DISTINCT FROM p_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'Change % is not an approved, unapplied % of connector %', p_change, p_step, p_connector USING ERRCODE = 'FY213';
  END IF;
END
$$;

-- An active Super admin installs connector p_connector, as approved by p_change when needed; it returns its ID.
CREATE FUNCTION fyniq.install_installation_connector(p_by uuid, p_change uuid, p_connector text, p_version text, p_hash text,
  p_secret_names text[], p_consent jsonb, p_configuration jsonb, p_position integer, p_at timestamptz) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  out uuid;
BEGIN
  PERFORM fyniq.require_super_admin(p_by);
  PERFORM fyniq.apply_installation_change(p_change, p_connector, NULL, 'install', p_at);
  INSERT INTO fyniq.installation_connector (connector, manifest_version, manifest_hash, secret_names, consent, state, configuration, position,
    installed_by, installed_at, changed_by, changed_at)
  VALUES (p_connector, p_version, p_hash, coalesce(p_secret_names, '{}'), coalesce(p_consent, '{}'), 'installed', coalesce(p_configuration, '{}'),
    p_position, 'user:' || p_by, p_at, 'user:' || p_by, p_at)
  RETURNING id INTO out;
  RETURN out;
END
$$;

-- The first-start bootstrap (rev 0.139): the email connectors from the deployment's settings, enabled, with their
-- secrets as references; only into an empty table, so it runs once and never overrides a Super admin (FY214). p_rows is
-- a JSON array of {connector, manifest_version, manifest_hash, secret_names, consent, configuration, position, secrets: {name: ref}}.
CREATE FUNCTION fyniq.bootstrap_installation_connectors(p_rows jsonb, p_at timestamptz) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  r jsonb;
  s record;
  id uuid;
  n integer := 0;
BEGIN
  IF EXISTS (SELECT 1 FROM fyniq.installation_connector) THEN
    RAISE EXCEPTION 'Connectors are installed already' USING ERRCODE = 'FY214', HINT = 'The bootstrap runs once, into an empty catalogue.';
  END IF;
  FOR r IN SELECT * FROM jsonb_array_elements(p_rows) LOOP
    INSERT INTO fyniq.installation_connector (connector, manifest_version, manifest_hash, secret_names, consent, state, configuration, position,
      installed_by, installed_at, changed_by, changed_at)
    VALUES (r->>'connector', r->>'manifest_version', r->>'manifest_hash',
      ARRAY(SELECT jsonb_array_elements_text(coalesce(r->'secret_names', '[]'))), coalesce(r->'consent', '{}'), 'enabled',
      coalesce(r->'configuration', '{}'), (r->>'position')::integer, 'service:api', p_at, 'service:api', p_at)
    RETURNING installation_connector.id INTO id;
    FOR s IN SELECT key, value FROM jsonb_each_text(coalesce(r->'secrets', '{}')) LOOP
      INSERT INTO fyniq.installation_connector_secret (connector_id, name, ref, set_by, set_at)
      VALUES (id, s.key, s.value, 'service:api', p_at);
    END LOOP;
    n := n + 1;
  END LOOP;
  RETURN n;
END
$$;

-- An active Super admin changes installation connector p_id at version p_version (CodeVersion otherwise): its state,
-- configuration, position, test result and consented manifest, as step p_step approved by p_change when needed.
CREATE FUNCTION fyniq.change_installation_connector(p_by uuid, p_change uuid, p_step text, p_id uuid, p_version integer, p_state text,
  p_configuration jsonb, p_position integer, p_test_passed boolean, p_test_reason text, p_tested_at timestamptz, p_manifest_version text,
  p_manifest_hash text, p_secret_names text[], p_consent jsonb, p_at timestamptz) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  c fyniq.installation_connector;
BEGIN
  PERFORM fyniq.require_super_admin(p_by);
  SELECT * INTO c FROM fyniq.installation_connector WHERE id = p_id FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'No connector %', p_id USING ERRCODE = 'P0002';
  END IF;
  IF c.version <> p_version THEN
    RAISE EXCEPTION 'Connector % changed since version %', c.connector, p_version USING ERRCODE = 'FY215';
  END IF;
  IF p_step NOT IN ('test', 'pause') THEN
    PERFORM fyniq.apply_installation_change(p_change, c.connector, p_id, p_step, p_at);
  ELSIF p_step = 'pause' AND (p_state <> 'paused' OR p_configuration IS DISTINCT FROM c.configuration OR p_position <> c.position
                              OR p_manifest_hash <> c.manifest_hash OR p_consent IS DISTINCT FROM c.consent) THEN
    RAISE EXCEPTION 'A pause pauses and changes nothing else' USING ERRCODE = 'FY207';
  ELSIF p_step = 'test' AND (p_state NOT IN (c.state, 'tested') OR p_configuration IS DISTINCT FROM c.configuration
                             OR p_position <> c.position OR p_manifest_hash <> c.manifest_hash OR p_consent IS DISTINCT FROM c.consent) THEN
    RAISE EXCEPTION 'A test records its result and nothing else' USING ERRCODE = 'FY207';
  END IF;
  UPDATE fyniq.installation_connector SET state = p_state, configuration = coalesce(p_configuration, '{}'), position = p_position,
    test_passed = p_test_passed, test_reason = p_test_reason, tested_at = p_tested_at, manifest_version = p_manifest_version,
    manifest_hash = p_manifest_hash, secret_names = coalesce(p_secret_names, '{}'), consent = coalesce(p_consent, '{}'), changed_by = 'user:' || p_by,
    changed_at = p_at,
    version = c.version + 1
   WHERE id = p_id;
  RETURN c.version + 1;
END
$$;

-- An active Super admin sets secret p_name of installation connector p_id to version p_ref, as approved by p_change when
-- needed: the current version stays valid until p_valid_until (the manifest's overlap), then is destroyed.
CREATE FUNCTION fyniq.set_installation_connector_secret(p_by uuid, p_change uuid, p_id uuid, p_name text, p_ref text,
  p_valid_until timestamptz, p_at timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  c fyniq.installation_connector;
BEGIN
  PERFORM fyniq.require_super_admin(p_by);
  SELECT * INTO c FROM fyniq.installation_connector WHERE id = p_id FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'No connector %', p_id USING ERRCODE = 'P0002';
  END IF;
  PERFORM fyniq.apply_installation_change(p_change, c.connector, p_id, 'secret', p_at);
  UPDATE fyniq.installation_connector_secret SET valid_until = greatest(p_valid_until, p_at)
   WHERE connector_id = p_id AND name = p_name AND valid_until IS NULL AND destroyed_at IS NULL;
  INSERT INTO fyniq.installation_connector_secret (connector_id, name, ref, set_by, set_at)
  VALUES (p_id, p_name, p_ref, 'user:' || p_by, p_at);
END
$$;

-- The background job marks a replaced secret version destroyed once its overlap has ended and Secret Manager destroyed
-- it; only such a version.
CREATE FUNCTION fyniq.destroyed_installation_connector_secret(p_ref text, p_at timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  UPDATE fyniq.installation_connector_secret s SET destroyed_at = p_at
   WHERE s.ref = p_ref AND s.destroyed_at IS NULL
     AND (s.valid_until <= p_at OR EXISTS (SELECT 1 FROM fyniq.installation_connector c WHERE c.id = s.connector_id AND c.state = 'uninstalled'));
  IF NOT FOUND THEN
    RAISE EXCEPTION 'Secret version % is not one to destroy', p_ref USING ERRCODE = 'FY208';
  END IF;
END
$$;

-- An active Super admin requests a change to an installation connector; it returns its ID.
CREATE FUNCTION fyniq.request_installation_change(p_by uuid, p_connector_id uuid, p_connector text, p_step text, p_summary text,
  p_change jsonb, p_envelope text, p_hash bytea, p_mfa_at timestamptz, p_at timestamptz, p_expires timestamptz) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  out uuid;
BEGIN
  PERFORM fyniq.require_super_admin(p_by);
  INSERT INTO fyniq.installation_change (connector_id, connector, step, summary, change, envelope, envelope_hash, requested_by, requested_at,
    requester_mfa_at, expires_at)
  VALUES (p_connector_id, p_connector, p_step, p_summary, p_change, p_envelope, p_hash, p_by, p_at, p_mfa_at, p_expires)
  RETURNING id INTO out;
  RETURN out;
END
$$;

-- Another active Super admin approves or rejects pending change p_id, whose envelope hash is p_hash (FY212 otherwise),
-- before it expires (FY211); never the person who asked (FY210).
CREATE FUNCTION fyniq.decide_installation_change(p_by uuid, p_id uuid, p_hash bytea, p_approve boolean, p_mfa_at timestamptz,
  p_reason text, p_at timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  ch fyniq.installation_change;
BEGIN
  PERFORM fyniq.require_super_admin(p_by);
  SELECT * INTO ch FROM fyniq.installation_change WHERE id = p_id FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'No change %', p_id USING ERRCODE = 'P0002';
  END IF;
  IF ch.requested_by = p_by THEN
    RAISE EXCEPTION 'The person who asked for a change cannot decide it' USING ERRCODE = 'FY210',
      HINT = 'The other Super admin decides it (FR-ADM-07).';
  END IF;
  IF ch.status <> 'pending' OR ch.expires_at <= p_at THEN
    RAISE EXCEPTION 'Change % is % and cannot be decided', p_id, CASE WHEN ch.status = 'pending' THEN 'expired' ELSE ch.status END
      USING ERRCODE = 'FY211';
  END IF;
  IF ch.envelope_hash <> p_hash THEN
    RAISE EXCEPTION 'The change decided is not the change requested' USING ERRCODE = 'FY212';
  END IF;
  UPDATE fyniq.installation_change SET status = CASE WHEN p_approve THEN 'approved' ELSE 'rejected' END, decided_by = p_by, decided_at = p_at,
    decider_mfa_at = p_mfa_at, reason = p_reason
   WHERE id = p_id;
END
$$;

-- The background job marks pending changes past their expiry as expired, and returns them.
CREATE FUNCTION fyniq.expire_installation_changes(p_at timestamptz) RETURNS SETOF uuid
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  UPDATE fyniq.installation_change SET status = 'expired' WHERE status = 'pending' AND expires_at <= p_at RETURNING id
$$;

-- The installation's connectors, live and uninstalled, for the API, the worker and the jobs.
CREATE FUNCTION fyniq.installation_connectors() RETURNS SETOF fyniq.installation_connector
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT * FROM fyniq.installation_connector ORDER BY position, installed_at, id
$$;

-- Every version of the installation connectors' secrets: what is current, what is still valid in its overlap, and what
-- is to destroy.
CREATE FUNCTION fyniq.installation_connector_secrets() RETURNS SETOF fyniq.installation_connector_secret
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT * FROM fyniq.installation_connector_secret ORDER BY connector_id, name, set_at, id
$$;

-- The installation's changes, newest first.
CREATE FUNCTION fyniq.installation_changes() RETURNS SETOF fyniq.installation_change
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT * FROM fyniq.installation_change ORDER BY requested_at DESC, id DESC
$$;
-- +goose StatementEnd

CREATE TRIGGER installation_connector_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.installation_connector
  FOR EACH ROW EXECUTE FUNCTION fyniq.connector_guard();
CREATE TRIGGER organisation_connector_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.organisation_connector
  FOR EACH ROW EXECUTE FUNCTION fyniq.connector_guard();
CREATE TRIGGER installation_connector_secret_guard BEFORE UPDATE OR DELETE ON fyniq.installation_connector_secret
  FOR EACH ROW EXECUTE FUNCTION fyniq.connector_secret_guard();
CREATE TRIGGER organisation_connector_secret_guard BEFORE UPDATE OR DELETE ON fyniq.organisation_connector_secret
  FOR EACH ROW EXECUTE FUNCTION fyniq.connector_secret_guard();
CREATE TRIGGER installation_connector_secret_named BEFORE INSERT ON fyniq.installation_connector_secret
  FOR EACH ROW EXECUTE FUNCTION fyniq.connector_secret_named();
CREATE TRIGGER organisation_connector_secret_named BEFORE INSERT ON fyniq.organisation_connector_secret
  FOR EACH ROW EXECUTE FUNCTION fyniq.connector_secret_named();
CREATE TRIGGER installation_change_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.installation_change
  FOR EACH ROW EXECUTE FUNCTION fyniq.installation_change_guard();
CREATE TRIGGER installation_connector_no_truncate BEFORE TRUNCATE ON fyniq.installation_connector
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER installation_connector_secret_no_truncate BEFORE TRUNCATE ON fyniq.installation_connector_secret
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER installation_change_no_truncate BEFORE TRUNCATE ON fyniq.installation_change
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER organisation_connector_no_truncate BEFORE TRUNCATE ON fyniq.organisation_connector
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER organisation_connector_secret_no_truncate BEFORE TRUNCATE ON fyniq.organisation_connector_secret
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.organisation_connector ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.organisation_connector FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.organisation_connector
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.organisation_connector_secret ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.organisation_connector_secret FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.organisation_connector_secret
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Every change to an organisation's connector is audited in the same transaction (AUD-01); the installation's are
-- recorded in the installation organisation's trail by the API.
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.organisation_connector
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('organisation_connector', 'id');

-- The approval engine covers an organisation's connector changes (FM-1).
ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_object_known CHECK (
  (object_type, operation) IN (('organisation_setting', 'organisation_setting.change'), ('membership', 'role.assign'),
                               ('membership', 'role.revoke'), ('member_invitation', 'member_invitation.create'),
                               ('approval_policy', 'approval_policy.change'), ('statutory_pack', 'statutory_pack.activate'),
                               ('team_mode', 'team_mode.change'), ('period', 'period.reopen'),
                               ('journal', 'journal.post'), ('journal', 'journal.reverse'), ('journal', 'journal.edit'),
                               ('opening_balance', 'opening_balance.edit'),
                               ('bill', 'bill.post'), ('bill', 'bill.void'), ('bill', 'bill.edit'),
                               ('bill_payment', 'bill_payment.void'),
                               ('credit_application', 'credit_application.void'),
                               ('supplier_refund', 'supplier_refund.void'),
                               ('purchase_order', 'purchase_order.approve'),
                               ('document_template', 'document_template.publish'),
                               ('invoice', 'invoice.post'), ('invoice', 'invoice.void'), ('invoice', 'invoice.edit'),
                               ('customer_receipt', 'customer_receipt.void'),
                               ('write_off', 'write_off.post'), ('write_off', 'write_off.void'),
                               ('customer_credit_application', 'customer_credit_application.void'),
                               ('customer_refund', 'customer_refund.void'),
                               ('customer_deposit', 'customer_deposit.void'),
                               ('recurring_invoice', 'recurring_invoice.approve'),
                               ('connector', 'connector.change'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector')) NOT VALID;

GRANT SELECT ON fyniq.organisation_connector, fyniq.organisation_connector_secret TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (connector, manifest_version, manifest_hash, secret_names, consent, state, configuration, position, installed_by, installed_at,
  changed_by, changed_at) ON fyniq.organisation_connector TO fyniq_api;
GRANT UPDATE (state, configuration, position, test_passed, test_reason, tested_at, manifest_version, manifest_hash, secret_names, consent,
  changed_by, changed_at, version) ON fyniq.organisation_connector TO fyniq_api;
GRANT INSERT (connector_id, name, ref, set_by, set_at) ON fyniq.organisation_connector_secret TO fyniq_api;
GRANT UPDATE (valid_until) ON fyniq.organisation_connector_secret TO fyniq_api;
GRANT UPDATE (destroyed_at) ON fyniq.organisation_connector_secret TO fyniq_worker;

REVOKE ALL ON FUNCTION fyniq.install_installation_connector(uuid, uuid, text, text, text, text[], jsonb, jsonb, integer, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION fyniq.bootstrap_installation_connectors(jsonb, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION fyniq.change_installation_connector(uuid, uuid, text, uuid, integer, text, jsonb, integer, boolean, text, timestamptz,
  text, text, text[], jsonb, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION fyniq.set_installation_connector_secret(uuid, uuid, uuid, text, text, timestamptz, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION fyniq.destroyed_installation_connector_secret(text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION fyniq.request_installation_change(uuid, uuid, text, text, text, jsonb, text, bytea, timestamptz, timestamptz,
  timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION fyniq.decide_installation_change(uuid, uuid, bytea, boolean, timestamptz, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION fyniq.expire_installation_changes(timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION fyniq.installation_connectors() FROM PUBLIC;
REVOKE ALL ON FUNCTION fyniq.installation_connector_secrets() FROM PUBLIC;
REVOKE ALL ON FUNCTION fyniq.installation_changes() FROM PUBLIC;
REVOKE ALL ON FUNCTION fyniq.installation_dual_control() FROM PUBLIC;
REVOKE ALL ON FUNCTION fyniq.apply_installation_change(uuid, text, uuid, text, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.install_installation_connector(uuid, uuid, text, text, text, text[], jsonb, jsonb, integer, timestamptz) TO fyniq_api;
GRANT EXECUTE ON FUNCTION fyniq.bootstrap_installation_connectors(jsonb, timestamptz) TO fyniq_api;
GRANT EXECUTE ON FUNCTION fyniq.change_installation_connector(uuid, uuid, text, uuid, integer, text, jsonb, integer, boolean, text, timestamptz,
  text, text, text[], jsonb, timestamptz) TO fyniq_api;
GRANT EXECUTE ON FUNCTION fyniq.set_installation_connector_secret(uuid, uuid, uuid, text, text, timestamptz, timestamptz) TO fyniq_api;
GRANT EXECUTE ON FUNCTION fyniq.destroyed_installation_connector_secret(text, timestamptz) TO fyniq_worker;
GRANT EXECUTE ON FUNCTION fyniq.request_installation_change(uuid, uuid, text, text, text, jsonb, text, bytea, timestamptz, timestamptz,
  timestamptz) TO fyniq_api;
GRANT EXECUTE ON FUNCTION fyniq.decide_installation_change(uuid, uuid, bytea, boolean, timestamptz, text, timestamptz) TO fyniq_api;
GRANT EXECUTE ON FUNCTION fyniq.expire_installation_changes(timestamptz) TO fyniq_worker;
GRANT EXECUTE ON FUNCTION fyniq.installation_connectors() TO fyniq_api, fyniq_worker;
GRANT EXECUTE ON FUNCTION fyniq.installation_connector_secrets() TO fyniq_api, fyniq_worker;
GRANT EXECUTE ON FUNCTION fyniq.installation_changes() TO fyniq_api, fyniq_worker;
