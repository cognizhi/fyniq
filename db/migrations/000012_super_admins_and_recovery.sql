-- The second Super admin, recovery codes and the deployment's two-person recovery (PRD-001 FR-ADM-06,
-- FR-ADM-07, FR-ADM-09, SEC-35; open question 36: two Super admins, one per founder, each keeping
-- printed recovery codes offline; the deployment recovery needs both people and is audited; CTL-IAM-07).
--
-- A grant is a one-time code, stored as its SHA-256 and used once:
--   invitation             a Super admin invites the second Super admin; redeeming it makes the
--                          redeemer one
--   recovery               ten printed codes per Super admin
--   recovery_confirmation  the other Super admin confirms a recovery (with step-up)
--   deployment_recovery    issued at the deployment by two named operators, for a Super admin
-- Recovery needs two independent proofs (SEC-35): a recovery code with the other Super admin's
-- confirmation, or the deployment recovery, which is two people already. Recovering replaces the Super
-- admin's sign-in identities with the one that recovered. P0 allows at most two Super admins; more
-- arrive with dual control (FR-ADM-07, P1). No change may leave the installation without one.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.super_admin_grant (
  code_hash  bytea       NOT NULL,
  kind       text        NOT NULL,
  user_id    uuid,                  -- the Super admin a recovery code, confirmation or deployment recovery is for
  created_by uuid,                  -- the Super admin who invited or confirmed
  issued_by  text,                  -- the two operators of a deployment recovery
  created_at timestamptz NOT NULL,
  expires_at timestamptz,
  used_at    timestamptz,
  used_by    uuid,
  CONSTRAINT super_admin_grant_pkey PRIMARY KEY (code_hash),
  CONSTRAINT super_admin_grant_code_sha256 CHECK (length(code_hash) = 32),
  CONSTRAINT super_admin_grant_kind_known CHECK (kind IN ('invitation', 'recovery', 'recovery_confirmation', 'deployment_recovery')),
  CONSTRAINT super_admin_grant_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.super_admin (user_id),
  CONSTRAINT super_admin_grant_created_by_fkey FOREIGN KEY (created_by) REFERENCES fyniq.super_admin (user_id),
  CONSTRAINT super_admin_grant_used_by_fkey FOREIGN KEY (used_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT super_admin_grant_shape CHECK (
    (kind = 'invitation' AND created_by IS NOT NULL AND user_id IS NULL AND issued_by IS NULL AND expires_at IS NOT NULL) OR
    (kind = 'recovery' AND user_id IS NOT NULL AND created_by IS NULL AND issued_by IS NULL AND expires_at IS NULL) OR
    (kind = 'recovery_confirmation' AND user_id IS NOT NULL AND created_by IS NOT NULL AND created_by <> user_id
      AND issued_by IS NULL AND expires_at IS NOT NULL) OR
    (kind = 'deployment_recovery' AND user_id IS NOT NULL AND created_by IS NULL AND length(issued_by) BETWEEN 3 AND 200
      AND expires_at IS NOT NULL)),
  CONSTRAINT super_admin_grant_used_whole CHECK ((used_at IS NULL) = (used_by IS NULL))
);
CREATE INDEX super_admin_grant_user_idx ON fyniq.super_admin_grant (user_id, kind);
CREATE INDEX super_admin_grant_created_by_idx ON fyniq.super_admin_grant (created_by);
CREATE INDEX super_admin_grant_used_by_idx ON fyniq.super_admin_grant (used_by);

-- At most two Super admins in P0, and never none; a record is removed, never changed or deleted.
-- +goose StatementBegin
CREATE FUNCTION fyniq.super_admin_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' AND (SELECT count(*) FROM fyniq.super_admin WHERE removed_at IS NULL) >= 2 THEN
    RAISE EXCEPTION 'An installation has at most two Super admins until dual control arrives (FR-ADM-07)' USING ERRCODE = 'FY084';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF NEW.user_id IS DISTINCT FROM OLD.user_id OR NEW.added_at IS DISTINCT FROM OLD.added_at
       OR NEW.added_by IS DISTINCT FROM OLD.added_by OR OLD.removed_at IS NOT NULL THEN
      RAISE EXCEPTION 'A Super admin record is removed, never changed' USING ERRCODE = 'FY083';
    END IF;
    IF NEW.removed_at IS NOT NULL
       AND NOT EXISTS (SELECT 1 FROM fyniq.super_admin WHERE removed_at IS NULL AND user_id <> NEW.user_id) THEN
      RAISE EXCEPTION 'The last Super admin cannot be removed, only replaced (FR-ADM-07)' USING ERRCODE = 'FY083';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER super_admin_guard BEFORE INSERT OR UPDATE ON fyniq.super_admin
  FOR EACH ROW EXECUTE FUNCTION fyniq.super_admin_guard();

-- +goose StatementBegin
CREATE FUNCTION fyniq.super_admin_no_delete() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'Super admins are removed, never deleted (% on %)', TG_OP, TG_TABLE_NAME USING ERRCODE = 'FY083';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER super_admin_no_delete BEFORE DELETE OR TRUNCATE ON fyniq.super_admin
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.super_admin_no_delete();

-- Every grant created or used has its event in the installation's trail, in the same transaction,
-- about the Super admin it concerns: the one it is for, the one who used it, or the one who invited.
-- One event covers a set of ten recovery codes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.require_grant_audit() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org      uuid := (SELECT installation_org FROM fyniq.installation WHERE singleton);
  previous text := current_setting('app.org_id', true);
  subject  uuid := coalesce(NEW.user_id, NEW.used_by, NEW.created_by);
  found    boolean;
BEGIN
  IF org IS NULL THEN
    RAISE EXCEPTION 'A Super admin grant changed before the installation''s trail exists' USING ERRCODE = 'FY061';
  END IF;
  PERFORM set_config('app.org_id', org::text, true);
  SELECT EXISTS (
    SELECT 1 FROM fyniq.audit_event e
     WHERE e.organisation_id = org AND e.created_xact = pg_current_xact_id()::text::bigint
       AND e.object_type = 'super_admin' AND e.object_id = subject) INTO found;
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
  IF NOT found THEN
    RAISE EXCEPTION 'A Super admin % (%) has no event in the installation''s trail', NEW.kind, TG_OP
      USING ERRCODE = 'FY061', HINT = 'Change Super admins and their codes through the Super admin commands (AUD-08).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.super_admin_grant
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_grant_audit();

-- ------------------------------------------------------------------ functions: reading
-- The active Super admin behind a sign-in identity, or NULL.
-- +goose StatementBegin
CREATE FUNCTION fyniq.super_admin_for_identity(p_issuer text, p_subject text) RETURNS uuid
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT s.user_id FROM fyniq.user_identity i JOIN fyniq.app_user u USING (user_id) JOIN fyniq.super_admin s USING (user_id)
   WHERE i.issuer = p_issuer AND i.subject = p_subject AND u.status = 'active' AND s.removed_at IS NULL
$$;
-- +goose StatementEnd

-- The installation organisation (its trail), or NULL before setup.
-- +goose StatementBegin
CREATE FUNCTION fyniq.installation_org() RETURNS uuid
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT installation_org FROM fyniq.installation WHERE singleton
$$;
-- +goose StatementEnd

-- The active Super admins who can sign in: the nightly check (CTL-IAM-07).
-- +goose StatementBegin
CREATE FUNCTION fyniq.count_super_admins() RETURNS integer
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT count(*)::integer FROM fyniq.super_admin s JOIN fyniq.app_user u USING (user_id)
   WHERE s.removed_at IS NULL AND u.status = 'active'
$$;
-- +goose StatementEnd

-- ------------------------------------------------------------------ functions: grants
-- The caller must be an active Super admin; raises FY085 otherwise.
-- +goose StatementBegin
CREATE FUNCTION fyniq.require_super_admin(p_user uuid) RETURNS void
LANGUAGE plpgsql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM fyniq.super_admin s JOIN fyniq.app_user u USING (user_id)
                  WHERE s.user_id = p_user AND s.removed_at IS NULL AND u.status = 'active') THEN
    RAISE EXCEPTION 'User % is not an active Super admin', p_user USING ERRCODE = 'FY085';
  END IF;
END
$$;
-- +goose StatementEnd

-- A Super admin invites the second one. A new invitation replaces an unused one.
-- +goose StatementBegin
CREATE FUNCTION fyniq.invite_super_admin(p_by uuid, p_hash bytea, p_now timestamptz, p_expires timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  PERFORM 1 FROM fyniq.installation WHERE singleton FOR UPDATE; -- one Super admin change at a time
  PERFORM fyniq.require_super_admin(p_by);
  IF (SELECT count(*) FROM fyniq.super_admin WHERE removed_at IS NULL) >= 2 THEN
    RAISE EXCEPTION 'An installation has at most two Super admins until dual control arrives (FR-ADM-07)' USING ERRCODE = 'FY084';
  END IF;
  DELETE FROM fyniq.super_admin_grant WHERE kind = 'invitation' AND used_at IS NULL;
  INSERT INTO fyniq.super_admin_grant (code_hash, kind, created_by, created_at, expires_at)
  VALUES (p_hash, 'invitation', p_by, p_now, p_expires);
END
$$;
-- +goose StatementEnd

-- Replace a Super admin's unused recovery codes with ten new ones.
-- +goose StatementBegin
CREATE FUNCTION fyniq.set_recovery_codes(p_user uuid, p_hashes bytea[], p_now timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  PERFORM fyniq.require_super_admin(p_user);
  IF cardinality(p_hashes) IS DISTINCT FROM 10 THEN
    RAISE EXCEPTION 'A Super admin has ten recovery codes, not %', cardinality(p_hashes) USING ERRCODE = 'FY085';
  END IF;
  DELETE FROM fyniq.super_admin_grant WHERE user_id = p_user AND kind = 'recovery' AND used_at IS NULL;
  INSERT INTO fyniq.super_admin_grant (code_hash, kind, user_id, created_at)
  SELECT h, 'recovery', p_user, p_now FROM unnest(p_hashes) AS h;
END
$$;
-- +goose StatementEnd

-- The other Super admin confirms a recovery: the second proof that goes with a recovery code (SEC-35).
-- A new confirmation replaces an unused one.
-- +goose StatementBegin
CREATE FUNCTION fyniq.confirm_super_admin_recovery(p_by uuid, p_for uuid, p_hash bytea, p_now timestamptz, p_expires timestamptz)
RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  PERFORM fyniq.require_super_admin(p_by);
  PERFORM fyniq.require_super_admin(p_for);
  IF p_by = p_for THEN
    RAISE EXCEPTION 'A Super admin cannot confirm their own recovery' USING ERRCODE = 'FY085';
  END IF;
  DELETE FROM fyniq.super_admin_grant WHERE user_id = p_for AND kind = 'recovery_confirmation' AND used_at IS NULL;
  INSERT INTO fyniq.super_admin_grant (code_hash, kind, user_id, created_by, created_at, expires_at)
  VALUES (p_hash, 'recovery_confirmation', p_for, p_by, p_now, p_expires);
END
$$;
-- +goose StatementEnd

-- The deployment's two-person recovery (docs/runbooks/super-admin-recovery.md): the migrator issues a
-- one-time code for one Super admin, naming the two operators. A new one replaces an unused one.
-- +goose StatementBegin
CREATE FUNCTION fyniq.issue_deployment_recovery(p_user uuid, p_hash bytea, p_operators text, p_now timestamptz, p_expires timestamptz)
RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  names text[] := regexp_split_to_array(btrim(p_operators), '\s*,\s*');
BEGIN
  PERFORM fyniq.require_super_admin(p_user);
  IF cardinality(names) <> 2 OR length(names[1]) < 1 OR length(names[2]) < 1 OR lower(names[1]) = lower(names[2]) THEN
    RAISE EXCEPTION 'A deployment recovery names its two operators, separated by a comma' USING ERRCODE = 'FY085';
  END IF;
  DELETE FROM fyniq.super_admin_grant WHERE user_id = p_user AND kind = 'deployment_recovery' AND used_at IS NULL;
  INSERT INTO fyniq.super_admin_grant (code_hash, kind, user_id, issued_by, created_at, expires_at)
  VALUES (p_hash, 'deployment_recovery', p_user, array_to_string(names, ', '), p_now, p_expires);
END
$$;
-- +goose StatementEnd

-- Redeem an invitation for the signed-in identity: the user behind it (or a new one) becomes a Super
-- admin. Every refusal is the same FY085, so a guess learns nothing.
-- +goose StatementBegin
CREATE FUNCTION fyniq.accept_super_admin_invitation(p_hash bytea, p_display_name text, p_issuer text, p_subject text,
  p_now timestamptz) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  g   fyniq.super_admin_grant;
  uid uuid;
BEGIN
  PERFORM 1 FROM fyniq.installation WHERE singleton FOR UPDATE; -- of two redemptions at once, one waits
  SELECT * INTO g FROM fyniq.super_admin_grant
   WHERE code_hash = p_hash AND kind = 'invitation' AND used_at IS NULL AND expires_at > p_now FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'The invitation is not valid, used or expired' USING ERRCODE = 'FY085';
  END IF;
  SELECT i.user_id INTO uid FROM fyniq.user_identity i WHERE i.issuer = p_issuer AND i.subject = p_subject;
  IF uid IS NULL THEN
    INSERT INTO fyniq.app_user (display_name) VALUES (p_display_name) RETURNING user_id INTO uid;
    INSERT INTO fyniq.user_identity (issuer, subject, user_id) VALUES (p_issuer, p_subject, uid);
  ELSIF EXISTS (SELECT 1 FROM fyniq.super_admin WHERE user_id = uid)
     OR NOT EXISTS (SELECT 1 FROM fyniq.app_user WHERE user_id = uid AND status = 'active') THEN
    RAISE EXCEPTION 'The invitation is not valid, used or expired' USING ERRCODE = 'FY085';
  END IF;
  INSERT INTO fyniq.super_admin (user_id, added_at, added_by) VALUES (uid, p_now, g.created_by);
  UPDATE fyniq.super_admin_grant SET used_at = p_now, used_by = uid WHERE code_hash = p_hash;
  RETURN uid;
END
$$;
-- +goose StatementEnd

-- Recover a Super admin with two proofs: a recovery code and the other Super admin's confirmation, or a
-- deployment recovery (a recovery code with it is optional). The signed-in identity becomes the Super
-- admin's only one; the identities it replaces are returned, so their tokens can be revoked (AI-38).
-- Every refusal is the same FY085.
-- +goose StatementBegin
CREATE FUNCTION fyniq.recover_super_admin(p_recovery_hash bytea, p_confirmation_hash bytea, p_issuer text, p_subject text,
  p_now timestamptz, OUT recovered uuid, OUT via text, OUT removed_subjects text[])
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  c     fyniq.super_admin_grant;
  owner uuid;
BEGIN
  SELECT * INTO c FROM fyniq.super_admin_grant
   WHERE code_hash = p_confirmation_hash AND kind IN ('recovery_confirmation', 'deployment_recovery')
     AND used_at IS NULL AND expires_at > p_now FOR UPDATE;
  IF NOT FOUND OR (c.kind = 'recovery_confirmation' AND p_recovery_hash IS NULL) THEN
    RAISE EXCEPTION 'The recovery is not valid, used or expired' USING ERRCODE = 'FY085';
  END IF;
  IF p_recovery_hash IS NOT NULL THEN
    PERFORM 1 FROM fyniq.super_admin_grant
     WHERE code_hash = p_recovery_hash AND kind = 'recovery' AND user_id = c.user_id AND used_at IS NULL FOR UPDATE;
    IF NOT FOUND THEN
      RAISE EXCEPTION 'The recovery is not valid, used or expired' USING ERRCODE = 'FY085';
    END IF;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.super_admin s JOIN fyniq.app_user u USING (user_id)
                  WHERE s.user_id = c.user_id AND s.removed_at IS NULL AND u.status = 'active') THEN
    RAISE EXCEPTION 'The recovery is not valid, used or expired' USING ERRCODE = 'FY085';
  END IF;
  SELECT i.user_id INTO owner FROM fyniq.user_identity i WHERE i.issuer = p_issuer AND i.subject = p_subject;
  IF owner IS NOT NULL AND owner <> c.user_id THEN
    RAISE EXCEPTION 'The recovery is not valid, used or expired' USING ERRCODE = 'FY085';
  END IF;

  recovered := c.user_id;
  via := c.kind;
  WITH gone AS (
    DELETE FROM fyniq.user_identity i
     WHERE i.user_id = c.user_id AND NOT (i.issuer = p_issuer AND i.subject = p_subject)
    RETURNING i.subject)
  SELECT coalesce(array_agg(gone.subject ORDER BY gone.subject), '{}') INTO removed_subjects FROM gone;
  IF owner IS NULL THEN
    INSERT INTO fyniq.user_identity (issuer, subject, user_id) VALUES (p_issuer, p_subject, c.user_id);
  END IF;
  UPDATE fyniq.super_admin_grant SET used_at = p_now, used_by = c.user_id
   WHERE code_hash IN (p_confirmation_hash, p_recovery_hash);
END
$$;
-- +goose StatementEnd

-- ------------------------------------------------------------------ grants (db/README.md)
REVOKE ALL ON FUNCTION fyniq.super_admin_for_identity(text, text), fyniq.installation_org(), fyniq.count_super_admins(),
  fyniq.require_super_admin(uuid), fyniq.invite_super_admin(uuid, bytea, timestamptz, timestamptz),
  fyniq.set_recovery_codes(uuid, bytea[], timestamptz),
  fyniq.confirm_super_admin_recovery(uuid, uuid, bytea, timestamptz, timestamptz),
  fyniq.issue_deployment_recovery(uuid, bytea, text, timestamptz, timestamptz),
  fyniq.accept_super_admin_invitation(bytea, text, text, text, timestamptz),
  fyniq.recover_super_admin(bytea, bytea, text, text, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.super_admin_for_identity(text, text), fyniq.invite_super_admin(uuid, bytea, timestamptz, timestamptz),
  fyniq.set_recovery_codes(uuid, bytea[], timestamptz), fyniq.confirm_super_admin_recovery(uuid, uuid, bytea, timestamptz, timestamptz),
  fyniq.accept_super_admin_invitation(bytea, text, text, text, timestamptz),
  fyniq.recover_super_admin(bytea, bytea, text, text, timestamptz) TO fyniq_api;
GRANT EXECUTE ON FUNCTION fyniq.installation_org(), fyniq.count_super_admins() TO fyniq_api, fyniq_worker;
-- issue_deployment_recovery: the migrator (its owner) only.
