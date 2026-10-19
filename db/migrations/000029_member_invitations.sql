-- Invitations to an organisation, and the founder bootstrap (PRD-001 FR-ADM-01, FM-1, rev 0.35; p1-users-roles task 2.1).
--
-- An invitation names who it is for and the roles they will hold; who made it is its audit event's actor (with an expiry for an external user). Its code
-- is shown once, valid 7 days, kept only as its SHA-256, works once and can be withdrawn while unused. Redeeming
-- it makes the signed-in person (a new user, or the one their sign-in belongs to) a member with those roles.
-- Who may invite and change roles (the Owner alone until a second member joins; then nobody until the approval
-- engine) is the application's rule; the database keeps the organisation from losing its last Owner.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.member_invitation (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  code_hash       bytea       NOT NULL,
  display_name    text        NOT NULL,
  roles           text[]      NOT NULL,
  roles_expire_at timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  expires_at      timestamptz NOT NULL,
  accepted_at     timestamptz,
  accepted_by     uuid,
  withdrawn_at    timestamptz,
  CONSTRAINT member_invitation_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT member_invitation_code_key UNIQUE (organisation_id, code_hash),
  CONSTRAINT member_invitation_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT member_invitation_accepted_by_fkey FOREIGN KEY (accepted_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT member_invitation_hash_length CHECK (octet_length(code_hash) = 32),
  CONSTRAINT member_invitation_name_length CHECK (length(btrim(display_name)) BETWEEN 1 AND 200),
  CONSTRAINT member_invitation_roles_known CHECK (cardinality(roles) BETWEEN 1 AND 16 AND roles <@ ARRAY['employee', 'approver',
    'bookkeeper', 'ar_specialist', 'ap_specialist', 'payment_releaser', 'payroll_administrator', 'accountant', 'controller', 'owner',
    'administrator', 'auditor', 'tax_agent', 'board_viewer', 'company_secretary', 'dpo']),
  CONSTRAINT member_invitation_owner_never_expires CHECK (roles_expire_at IS NULL OR NOT 'owner' = ANY (roles)),
  CONSTRAINT member_invitation_expiry_after_creation CHECK (expires_at > created_at AND (roles_expire_at IS NULL OR roles_expire_at > created_at)),
  CONSTRAINT member_invitation_used_once CHECK ((accepted_at IS NULL) = (accepted_by IS NULL) AND (accepted_at IS NULL OR withdrawn_at IS NULL))
);

-- An invitation is withdrawn or accepted once, never changed otherwise.
-- +goose StatementBegin
CREATE FUNCTION fyniq.member_invitation_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF OLD.accepted_at IS NOT NULL OR OLD.withdrawn_at IS NOT NULL
     OR (NEW.code_hash, NEW.display_name, NEW.roles, NEW.roles_expire_at, NEW.created_at, NEW.expires_at)
        IS DISTINCT FROM (OLD.code_hash, OLD.display_name, OLD.roles, OLD.roles_expire_at, OLD.created_at, OLD.expires_at) THEN
    RAISE EXCEPTION 'An invitation is accepted or withdrawn once, and never changed' USING ERRCODE = 'FY092';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER member_invitation_guard BEFORE UPDATE ON fyniq.member_invitation
  FOR EACH ROW EXECUTE FUNCTION fyniq.member_invitation_guard();

-- The organisation always keeps an Owner: the last live Owner assignment of an active member is never revoked.
-- +goose StatementBegin
CREATE FUNCTION fyniq.last_owner_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF OLD.role = 'owner' AND OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM fyniq.role_assignment r
         JOIN fyniq.user_membership m ON m.organisation_id = r.organisation_id AND m.id = r.membership_id
        WHERE r.organisation_id = NEW.organisation_id AND r.role = 'owner' AND r.id <> NEW.id AND r.revoked_at IS NULL
          AND m.status = 'active' AND (r.expires_at IS NULL OR r.expires_at > now())) THEN
    RAISE EXCEPTION 'The organisation''s last Owner keeps the role' USING ERRCODE = 'FY093',
      HINT = 'Make someone else Owner first (FR-ADM-09).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER last_owner_guard BEFORE UPDATE OF revoked_at ON fyniq.role_assignment
  FOR EACH ROW EXECUTE FUNCTION fyniq.last_owner_guard();

ALTER TABLE fyniq.member_invitation ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.member_invitation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.member_invitation
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.member_invitation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('member_invitation', 'id');

-- The display names of the organisation's members: app_user is the installation's, so the API reads only its
-- own members' names through this.
-- +goose StatementBegin
CREATE FUNCTION fyniq.member_names() RETURNS TABLE (user_id uuid, display_name text)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT u.user_id, u.display_name FROM fyniq.app_user u
   WHERE EXISTS (SELECT 1 FROM fyniq.user_membership m WHERE m.organisation_id = fyniq.current_org() AND m.user_id = u.user_id)
$$;
-- +goose StatementEnd

-- Redeem an invitation for the signed-in identity: the user behind it (or a new one) becomes a member with the
-- invitation's roles. The invitation is found in whichever organisation holds it, under that organisation's
-- row-level security, which stays set for the caller to audit the change. Every refusal is the same FY092, so a
-- guess learns nothing.
-- +goose StatementBegin
CREATE FUNCTION fyniq.accept_member_invitation(p_hash bytea, p_display_name text, p_issuer text, p_subject text, p_now timestamptz,
  OUT organisation_id uuid, OUT invitation_id uuid, OUT user_id uuid, OUT new_user boolean, OUT membership_id uuid)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid;
  inv fyniq.member_invitation;
  r   text;
BEGIN
  FOR org IN SELECT t.organisation_id FROM fyniq.tenant_directory t ORDER BY t.created_at, t.organisation_id LOOP
    PERFORM set_config('app.org_id', org::text, true);
    SELECT * INTO inv FROM fyniq.member_invitation i
     WHERE i.organisation_id = org AND i.code_hash = p_hash AND i.accepted_at IS NULL AND i.withdrawn_at IS NULL AND i.expires_at > p_now
       FOR UPDATE;
    EXIT WHEN FOUND;
  END LOOP;
  IF inv.id IS NULL THEN
    PERFORM set_config('app.org_id', '', true);
    RAISE EXCEPTION 'The invitation is not valid, used or expired' USING ERRCODE = 'FY092';
  END IF;
  accept_member_invitation.organisation_id := inv.organisation_id;
  accept_member_invitation.invitation_id := inv.id;
  accept_member_invitation.user_id := fyniq.user_for_identity(p_issuer, p_subject);
  accept_member_invitation.new_user := accept_member_invitation.user_id IS NULL;
  IF accept_member_invitation.new_user THEN
    INSERT INTO fyniq.app_user (display_name) VALUES (p_display_name) RETURNING app_user.user_id INTO accept_member_invitation.user_id;
    INSERT INTO fyniq.user_identity (issuer, subject, user_id) VALUES (p_issuer, p_subject, accept_member_invitation.user_id);
  ELSIF NOT EXISTS (SELECT 1 FROM fyniq.app_user u WHERE u.user_id = accept_member_invitation.user_id AND u.status = 'active')
     OR EXISTS (SELECT 1 FROM fyniq.user_membership m
                 WHERE m.organisation_id = inv.organisation_id AND m.user_id = accept_member_invitation.user_id) THEN
    RAISE EXCEPTION 'The invitation is not valid, used or expired' USING ERRCODE = 'FY092';
  END IF;
  -- The members' lock: a role change counting the members waits for this join, and the other way round.
  PERFORM pg_advisory_xact_lock(hashtextextended('fyniq.members:' || inv.organisation_id::text, 0));
  INSERT INTO fyniq.user_membership (user_id) VALUES (accept_member_invitation.user_id)
  RETURNING id INTO accept_member_invitation.membership_id;
  FOREACH r IN ARRAY inv.roles LOOP
    INSERT INTO fyniq.role_assignment (membership_id, role, expires_at)
    VALUES (accept_member_invitation.membership_id, r, inv.roles_expire_at);
  END LOOP;
  UPDATE fyniq.member_invitation i SET accepted_at = p_now, accepted_by = accept_member_invitation.user_id
   WHERE i.organisation_id = inv.organisation_id AND i.id = inv.id;
END
$$;
-- +goose StatementEnd

-- Only the API invites, withdraws and redeems; every role reads.
GRANT SELECT ON fyniq.member_invitation TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (code_hash, display_name, roles, roles_expire_at, expires_at), UPDATE (withdrawn_at)
  ON fyniq.member_invitation TO fyniq_api;
REVOKE ALL ON FUNCTION fyniq.member_names(), fyniq.accept_member_invitation(bytea, text, text, text, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.member_names(), fyniq.accept_member_invitation(bytea, text, text, text, timestamptz) TO fyniq_api;
