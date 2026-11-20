-- A sandbox organisation (PRD-001 FR-INT-18; p2-api-third-parties task 6.1; the owner's decision of 21 Nov 2026): a flagged organisation of
-- the same installation where an integrator tests against the sample company with API clients of its own, enabled per deployment. It holds
-- only synthetic data and sends nothing outside (the API, the connector runtime and the outbox refuse or stub every outbound path for it).
--
-- THE FLAG IS STRUCTURAL: it is a row of a registry that no application role can write, made only by create_sandbox_organisation, which
-- makes a NEW organisation in the same statement. A trigger refuses a row for an organisation that existed before the transaction, so no
-- real organisation becomes a sandbox, and another refuses every update and delete, so no sandbox becomes a real organisation (FY960).
-- Only an active Super admin makes one, and with two Super admins only as the other's approved installation change of step sandbox
-- (FR-ADM-07, FY213), as a private endpoint is registered (000310). The creator becomes its Owner, as for a practice organisation.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.installation_change DROP CONSTRAINT installation_change_step_known;
ALTER TABLE fyniq.installation_change ADD CONSTRAINT installation_change_step_known
  CHECK (step IN ('install', 'configure', 'secret', 'enable', 'resume', 'disable', 'uninstall', 'order', 'consent', 'sign_in',
    'private_endpoint', 'sandbox')) NOT VALID;

-- The registry: global, like the tenant directory, and read by no application role.
CREATE TABLE fyniq.sandbox_organisation (
  organisation_id uuid        NOT NULL,
  created_by      uuid        NOT NULL,
  change_id       uuid,
  created_at      timestamptz NOT NULL,
  CONSTRAINT sandbox_organisation_pkey PRIMARY KEY (organisation_id),
  CONSTRAINT sandbox_organisation_org_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT sandbox_organisation_user_fkey FOREIGN KEY (created_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT sandbox_organisation_change_fkey FOREIGN KEY (change_id) REFERENCES fyniq.installation_change (id)
);
REVOKE ALL ON fyniq.sandbox_organisation FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION fyniq.sandbox_organisation_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A sandbox organisation stays a sandbox organisation: the flag is never changed or removed' USING ERRCODE = 'FY960';
  END IF;
  -- Only an organisation made in this very transaction: a real organisation is never flagged afterwards.
  IF NOT EXISTS (SELECT 1 FROM fyniq.tenant_directory t WHERE t.organisation_id = NEW.organisation_id AND t.created_at >= transaction_timestamp()) THEN
    RAISE EXCEPTION 'Only a new organisation is made a sandbox organisation' USING ERRCODE = 'FY960';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER sandbox_organisation_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.sandbox_organisation
  FOR EACH ROW EXECUTE FUNCTION fyniq.sandbox_organisation_guard();

-- Whether an organisation is a sandbox one: a boolean for any role that does work in an organisation, and nothing else of the registry.
-- +goose StatementBegin
CREATE FUNCTION fyniq.organisation_is_sandbox(p_org uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM fyniq.sandbox_organisation s WHERE s.organisation_id = p_org)
$$;
-- +goose StatementEnd

-- An active Super admin makes sandbox organisation p_organisation (an ID the caller chose, so that the approval names it), as approved by
-- p_change when two Super admins must agree. The creator becomes its Owner. The name always starts with "Sandbox: ".
-- +goose StatementBegin
CREATE FUNCTION fyniq.create_sandbox_organisation(p_by uuid, p_change uuid, p_organisation uuid, p_name text, p_currency text, p_at timestamptz,
  OUT membership_id uuid, OUT assignment_id uuid)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  previous text := current_setting('app.org_id', true);
  minor    smallint;
BEGIN
  PERFORM fyniq.require_super_admin(p_by);
  PERFORM fyniq.apply_installation_change(p_change, 'sandbox:' || p_organisation, NULL, 'sandbox', p_at);
  IF p_name IS NULL OR left(p_name, 9) <> 'Sandbox: ' THEN
    RAISE EXCEPTION 'A sandbox organisation''s name starts with "Sandbox: "' USING ERRCODE = 'FY961';
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.tenant_directory t WHERE t.organisation_id = p_organisation) THEN
    RAISE EXCEPTION 'Organisation % exists already: only a new organisation is made a sandbox organisation', p_organisation USING ERRCODE = 'FY960';
  END IF;
  SELECT c.minor_units INTO minor FROM fyniq.currencies c WHERE c.code = p_currency;
  IF minor IS NULL THEN
    RAISE EXCEPTION 'currency % is not an ISO 4217 code Fyniq knows', p_currency USING ERRCODE = 'FY961';
  END IF;
  PERFORM set_config('app.org_id', p_organisation::text, true);
  INSERT INTO fyniq.organisation (organisation_id, name, base_currency, base_minor_units) VALUES (p_organisation, p_name, p_currency, minor);
  INSERT INTO fyniq.user_membership (user_id) VALUES (p_by) RETURNING id INTO membership_id;
  INSERT INTO fyniq.role_assignment (membership_id, role) VALUES (membership_id, 'owner') RETURNING id INTO assignment_id;
  INSERT INTO fyniq.sandbox_organisation (organisation_id, created_by, change_id, created_at) VALUES (p_organisation, p_by, p_change, p_at);
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.organisation_is_sandbox(uuid), fyniq.sandbox_organisation_guard(),
  fyniq.create_sandbox_organisation(uuid, uuid, uuid, text, text, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.organisation_is_sandbox(uuid) TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT EXECUTE ON FUNCTION fyniq.create_sandbox_organisation(uuid, uuid, uuid, text, text, timestamptz) TO fyniq_api;
