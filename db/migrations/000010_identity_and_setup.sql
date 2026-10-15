-- Users, Super admins, first-run setup and memberships (PRD-001 FR-ADM-06/07/08, §11.4; open
-- question 36; p0-identity-oauth-first-run).
--
-- Users are the installation's, not an organisation's: one person can belong to several organisations.
-- A user is known by sign-in identities, (issuer, subject) pairs; an email address is never the key
-- (FR-ADM-08). The global tables here follow job_slot's model: no application role is granted them;
-- services reach them only through the SECURITY DEFINER functions below.
--
-- First-run setup is done once, with a one-time code created at deployment (stored as its SHA-256):
-- the first person to complete it becomes the Super admin and the Owner of the first organisation.
-- Installation events (setup, Super admins) go to the installation's own trail: a reserved
-- organisation of kind 'installation' that holds only audit events, anchored and verified like any
-- other (founder decision, 15 Oct 2026). Memberships and role assignments are the organisation's.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- ------------------------------------------------------------------ global: users and identities
CREATE TABLE fyniq.app_user (
  user_id      uuid        NOT NULL DEFAULT uuidv7(),
  display_name text        NOT NULL,
  status       text        NOT NULL DEFAULT 'active',
  created_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT app_user_pkey PRIMARY KEY (user_id),
  CONSTRAINT app_user_display_name_length CHECK (length(display_name) BETWEEN 1 AND 200),
  CONSTRAINT app_user_status_known CHECK (status IN ('active', 'deactivated'))
);

CREATE TABLE fyniq.user_identity (
  issuer     text        NOT NULL,
  subject    text        NOT NULL,
  user_id    uuid        NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT user_identity_pkey PRIMARY KEY (issuer, subject),
  CONSTRAINT user_identity_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT user_identity_issuer_length CHECK (length(issuer) BETWEEN 1 AND 500),
  CONSTRAINT user_identity_subject_length CHECK (length(subject) BETWEEN 1 AND 500)
);
CREATE INDEX user_identity_user_idx ON fyniq.user_identity (user_id);

-- ------------------------------------------------------------------ global: the installation
CREATE TABLE fyniq.installation (
  singleton          boolean     NOT NULL DEFAULT true,
  setup_code_hash    bytea,
  setup_completed_at timestamptz,
  installation_org   uuid,
  CONSTRAINT installation_pkey PRIMARY KEY (singleton),
  CONSTRAINT installation_one_row CHECK (singleton),
  CONSTRAINT installation_code_sha256 CHECK (length(setup_code_hash) = 32),
  -- A code waits for setup, or setup is done; never both.
  CONSTRAINT installation_code_or_done CHECK ((setup_code_hash IS NULL) <> (setup_completed_at IS NULL))
  -- installation_org has no foreign key: a key into a tenant table must lead with organisation_id
  -- (§11.4). Only organisation_kind_guard writes it, with the id of the organisation being inserted in
  -- the same transaction.
);

CREATE TABLE fyniq.super_admin (
  user_id    uuid        NOT NULL,
  added_at   timestamptz NOT NULL DEFAULT now(),
  added_by   uuid,
  removed_at timestamptz,
  CONSTRAINT super_admin_pkey PRIMARY KEY (user_id),
  CONSTRAINT super_admin_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT super_admin_added_by_fkey FOREIGN KEY (added_by) REFERENCES fyniq.app_user (user_id)
);

-- ------------------------------------------------------------------ organisations: their kind
-- A company keeps books; the one installation organisation holds the installation's audit trail only.
ALTER TABLE fyniq.organisation ADD COLUMN kind text NOT NULL DEFAULT 'company';
ALTER TABLE fyniq.organisation ADD CONSTRAINT organisation_kind_known CHECK (kind IN ('company', 'installation')) NOT VALID;
-- 000011 validates it, in a transaction of its own (squawk: constraint-missing-not-valid).

-- There is one installation organisation, and only once setup has begun: creating one claims the
-- installation's slot for it, atomically.
-- +goose StatementBegin
CREATE FUNCTION fyniq.organisation_kind_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'UPDATE' AND NEW.kind IS DISTINCT FROM OLD.kind THEN
    RAISE EXCEPTION 'An organisation''s kind cannot change' USING ERRCODE = 'FY081';
  END IF;
  IF TG_OP = 'INSERT' AND NEW.kind = 'installation' THEN
    UPDATE fyniq.installation SET installation_org = NEW.organisation_id
     WHERE singleton AND installation_org IS NULL AND setup_completed_at IS NOT NULL;
    IF NOT FOUND THEN
      RAISE EXCEPTION 'The installation organisation exists already, or setup is not under way' USING ERRCODE = 'FY081';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER organisation_kind_guard BEFORE INSERT OR UPDATE ON fyniq.organisation
  FOR EACH ROW EXECUTE FUNCTION fyniq.organisation_kind_guard();

-- ------------------------------------------------------------------ tenant: memberships and roles
CREATE TABLE fyniq.user_membership (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  user_id         uuid        NOT NULL,
  status          text        NOT NULL DEFAULT 'active',
  created_at      timestamptz NOT NULL DEFAULT now(),
  version         integer     NOT NULL DEFAULT 1,
  CONSTRAINT user_membership_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT user_membership_user_key UNIQUE (organisation_id, user_id),
  CONSTRAINT user_membership_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT user_membership_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT user_membership_status_known CHECK (status IN ('active', 'removed')),
  CONSTRAINT user_membership_version_positive CHECK (version > 0)
);

-- The roles of PRD-001 §7.1 a person can hold (internal/authz.Roles less the Line manager, a
-- relationship, and the HR officer, a Nemura role). A test keeps the list equal to authz.
CREATE TABLE fyniq.role_assignment (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  membership_id   uuid        NOT NULL,
  role            text        NOT NULL,
  expires_at      timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  revoked_at      timestamptz,
  CONSTRAINT role_assignment_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT role_assignment_membership_fkey FOREIGN KEY (organisation_id, membership_id)
    REFERENCES fyniq.user_membership (organisation_id, id),
  CONSTRAINT role_assignment_role_known CHECK (role IN ('employee', 'approver', 'bookkeeper', 'ar_specialist',
    'ap_specialist', 'payment_releaser', 'payroll_administrator', 'accountant', 'controller', 'owner', 'administrator',
    'auditor', 'tax_agent', 'board_viewer', 'company_secretary', 'dpo')),
  CONSTRAINT role_assignment_expiry_after_creation CHECK (expires_at > created_at),
  CONSTRAINT role_assignment_revoked_after_creation CHECK (revoked_at >= created_at)
);
CREATE INDEX role_assignment_membership_idx ON fyniq.role_assignment (organisation_id, membership_id);
CREATE UNIQUE INDEX role_assignment_one_live ON fyniq.role_assignment (organisation_id, membership_id, role)
  WHERE revoked_at IS NULL;

-- Nobody is a member of the installation organisation; an assignment is revoked, never changed.
-- +goose StatementBegin
CREATE FUNCTION fyniq.membership_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_TABLE_NAME = 'user_membership' THEN
    IF EXISTS (SELECT 1 FROM fyniq.organisation o WHERE o.organisation_id = NEW.organisation_id AND o.kind = 'installation') THEN
      RAISE EXCEPTION 'Nobody is a member of the installation organisation' USING ERRCODE = 'FY082';
    END IF;
    IF TG_OP = 'UPDATE' AND (NEW.user_id IS DISTINCT FROM OLD.user_id OR NEW.created_at IS DISTINCT FROM OLD.created_at) THEN
      RAISE EXCEPTION 'A membership''s user cannot change' USING ERRCODE = 'FY082';
    END IF;
  ELSIF TG_OP = 'UPDATE' AND (NEW.role IS DISTINCT FROM OLD.role OR NEW.membership_id IS DISTINCT FROM OLD.membership_id
        OR NEW.expires_at IS DISTINCT FROM OLD.expires_at OR NEW.created_at IS DISTINCT FROM OLD.created_at
        OR OLD.revoked_at IS NOT NULL) THEN
    RAISE EXCEPTION 'A role assignment is revoked and assigned again, never changed' USING ERRCODE = 'FY082';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER membership_guard BEFORE INSERT OR UPDATE ON fyniq.user_membership
  FOR EACH ROW EXECUTE FUNCTION fyniq.membership_guard();
CREATE TRIGGER role_assignment_guard BEFORE UPDATE ON fyniq.role_assignment
  FOR EACH ROW EXECUTE FUNCTION fyniq.membership_guard();

-- ------------------------------------------------------------------ AUD-08 across organisations
-- First-run setup writes to two organisations in one transaction. The audit check runs at commit, under
-- the row-level security of the tenant set last, so it now looks in the row's own organisation.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.require_audit() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  audited_type text := TG_ARGV[0];
  audited_id   uuid := (to_jsonb(NEW) ->> TG_ARGV[1])::uuid;
  previous     text := current_setting('app.org_id', true);
  found        boolean;
BEGIN
  PERFORM set_config('app.org_id', NEW.organisation_id::text, true);
  SELECT EXISTS (
    SELECT 1 FROM fyniq.audit_event e
     WHERE e.organisation_id = NEW.organisation_id
       AND e.created_xact = pg_current_xact_id()::text::bigint
       AND e.object_type = audited_type
       AND e.object_id = audited_id) INTO found;
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
  IF NOT found THEN
    RAISE EXCEPTION 'The change to % % has no audit event in its transaction', audited_type, audited_id
      USING ERRCODE = 'FY061',
            HINT = 'Write changes through the unit of work, which records their audit events (AUD-08).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.user_membership
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('user_membership', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.role_assignment
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('role_assignment', 'id');

-- A Super admin added has its event in the installation's trail, in the same transaction.
-- +goose StatementBegin
CREATE FUNCTION fyniq.require_installation_audit() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org      uuid := (SELECT installation_org FROM fyniq.installation WHERE singleton);
  previous text := current_setting('app.org_id', true);
  found    boolean;
BEGIN
  IF org IS NULL THEN
    RAISE EXCEPTION 'A Super admin changed before the installation''s trail exists' USING ERRCODE = 'FY061';
  END IF;
  PERFORM set_config('app.org_id', org::text, true);
  SELECT EXISTS (
    SELECT 1 FROM fyniq.audit_event e
     WHERE e.organisation_id = org AND e.created_xact = pg_current_xact_id()::text::bigint
       AND e.object_type = 'super_admin' AND e.object_id = NEW.user_id) INTO found;
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
  IF NOT found THEN
    RAISE EXCEPTION 'The change to Super admin % has no event in the installation''s trail', NEW.user_id
      USING ERRCODE = 'FY061', HINT = 'Change Super admins through the setup and Super admin commands (AUD-08).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.super_admin
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_installation_audit();

-- ------------------------------------------------------------------ functions: setup and identity
-- The deployment sets the one-time setup code (its SHA-256), until setup is done; never afterwards.
-- +goose StatementBegin
CREATE FUNCTION fyniq.set_setup_code(p_hash bytea) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM fyniq.installation WHERE singleton AND setup_completed_at IS NOT NULL) THEN
    RAISE EXCEPTION 'Setup is done; the installation takes no new setup code' USING ERRCODE = 'FY080';
  END IF;
  INSERT INTO fyniq.installation (singleton, setup_code_hash) VALUES (true, p_hash)
  ON CONFLICT (singleton) DO UPDATE SET setup_code_hash = excluded.setup_code_hash;
END
$$;
-- +goose StatementEnd

-- Consume the setup code and create the first Super admin with their sign-in identity, atomically: of
-- two setups with the same code, one waits on the row lock and then finds the code gone.
-- +goose StatementBegin
CREATE FUNCTION fyniq.claim_setup(p_hash bytea, p_display_name text, p_issuer text, p_subject text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  uid uuid;
BEGIN
  UPDATE fyniq.installation SET setup_code_hash = NULL, setup_completed_at = now()
   WHERE singleton AND setup_completed_at IS NULL AND setup_code_hash = p_hash;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'The setup code is not valid, or setup is done' USING ERRCODE = 'FY080';
  END IF;
  INSERT INTO fyniq.app_user (display_name) VALUES (p_display_name) RETURNING user_id INTO uid;
  INSERT INTO fyniq.user_identity (issuer, subject, user_id) VALUES (p_issuer, p_subject, uid);
  INSERT INTO fyniq.super_admin (user_id) VALUES (uid);
  RETURN uid;
END
$$;
-- +goose StatementEnd

-- Whether setup is done: the only thing anyone may ask without signing in.
-- +goose StatementBegin
CREATE FUNCTION fyniq.setup_done() RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT coalesce((SELECT setup_completed_at IS NOT NULL FROM fyniq.installation WHERE singleton), false)
$$;
-- +goose StatementEnd

-- The user behind a sign-in identity, or NULL.
-- +goose StatementBegin
CREATE FUNCTION fyniq.user_for_identity(p_issuer text, p_subject text) RETURNS uuid
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT i.user_id FROM fyniq.user_identity i JOIN fyniq.app_user u USING (user_id)
   WHERE i.issuer = p_issuer AND i.subject = p_subject AND u.status = 'active'
$$;
-- +goose StatementEnd

-- ------------------------------------------------------------------ row-level security
ALTER TABLE fyniq.user_membership ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.user_membership FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.user_membership
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

ALTER TABLE fyniq.role_assignment ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.role_assignment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.role_assignment
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- ------------------------------------------------------------------ grants (db/README.md)
REVOKE ALL ON FUNCTION fyniq.set_setup_code(bytea), fyniq.claim_setup(bytea, text, text, text), fyniq.setup_done(),
  fyniq.user_for_identity(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.claim_setup(bytea, text, text, text), fyniq.setup_done(), fyniq.user_for_identity(text, text)
  TO fyniq_api;
GRANT INSERT (kind) ON fyniq.organisation TO fyniq_api;
GRANT SELECT ON fyniq.user_membership, fyniq.role_assignment TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (user_id) , UPDATE (status, version) ON fyniq.user_membership TO fyniq_api;
GRANT INSERT (membership_id, role, expires_at), UPDATE (revoked_at) ON fyniq.role_assignment TO fyniq_api;
