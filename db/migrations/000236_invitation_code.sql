-- A join finds its invitation by its code's hash (PRD-001 FR-ADM-01): the registry invitation_code maps a hash to the
-- organisation that holds it, written by member_invitation's trigger, so accept_member_invitation looks only there
-- instead of in every organisation in turn, which grew with their number. Only hashes and organisation IDs: a global
-- table no application role reads or writes, reached only through the function; a row never changes or goes (FY269).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.invitation_code (
  code_hash bytea NOT NULL,
  owner     uuid  NOT NULL,
  CONSTRAINT invitation_code_pkey PRIMARY KEY (code_hash, owner),
  CONSTRAINT invitation_code_hash_length CHECK (octet_length(code_hash) = 32)
);
REVOKE ALL ON fyniq.invitation_code FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION fyniq.invitation_code_registered() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  INSERT INTO fyniq.invitation_code (code_hash, owner) VALUES (NEW.code_hash, NEW.organisation_id);
  RETURN NULL;
END
$$;

CREATE FUNCTION fyniq.invitation_code_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'An invitation''s registry entry never changes or goes'
    USING ERRCODE = 'FY269', HINT = 'Withdraw the invitation instead (FR-ADM-01).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER invitation_code_registered AFTER INSERT ON fyniq.member_invitation
  FOR EACH ROW EXECUTE FUNCTION fyniq.invitation_code_registered();
CREATE TRIGGER invitation_code_guard BEFORE UPDATE OR DELETE ON fyniq.invitation_code
  FOR EACH ROW EXECUTE FUNCTION fyniq.invitation_code_guard();
CREATE TRIGGER invitation_code_no_truncate BEFORE TRUNCATE ON fyniq.invitation_code
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

-- The invitations made before this migration. The owner reads them past FORCE ROW LEVEL SECURITY for this one statement.
ALTER TABLE fyniq.member_invitation NO FORCE ROW LEVEL SECURITY;
INSERT INTO fyniq.invitation_code (code_hash, owner) SELECT code_hash, organisation_id FROM fyniq.member_invitation;
ALTER TABLE fyniq.member_invitation FORCE ROW LEVEL SECURITY;

-- The join looks only in the organisations that hold the code's hash; otherwise as before (rev 0.35, 000196).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.accept_member_invitation(p_hash bytea, p_display_name text, p_issuer text, p_subject text, p_now timestamptz,
  OUT organisation_id uuid, OUT invitation_id uuid, OUT user_id uuid, OUT new_user boolean, OUT membership_id uuid)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid;
  inv fyniq.member_invitation;
  r   text;
BEGIN
  FOR org IN SELECT c.owner FROM fyniq.invitation_code c WHERE c.code_hash = p_hash ORDER BY c.owner LOOP
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
                 WHERE m.organisation_id = inv.organisation_id AND m.user_id = accept_member_invitation.user_id AND m.status = 'active') THEN
    RAISE EXCEPTION 'The invitation is not valid, used or expired' USING ERRCODE = 'FY092';
  END IF;
  -- The members' lock: a role change counting the members waits for this join, and the other way round.
  PERFORM pg_advisory_xact_lock(hashtextextended('fyniq.members:' || inv.organisation_id::text, 0));
  UPDATE fyniq.user_membership m SET status = 'active', version = m.version + 1
   WHERE m.organisation_id = inv.organisation_id AND m.user_id = accept_member_invitation.user_id AND m.status = 'removed'
  RETURNING m.id INTO accept_member_invitation.membership_id;
  IF NOT FOUND THEN
    INSERT INTO fyniq.user_membership (user_id) VALUES (accept_member_invitation.user_id)
    RETURNING id INTO accept_member_invitation.membership_id;
  END IF;
  FOREACH r IN ARRAY inv.roles LOOP
    INSERT INTO fyniq.role_assignment (membership_id, role, expires_at)
    VALUES (accept_member_invitation.membership_id, r, inv.roles_expire_at);
  END LOOP;
  UPDATE fyniq.member_invitation i SET accepted_at = p_now, accepted_by = accept_member_invitation.user_id
   WHERE i.organisation_id = inv.organisation_id AND i.id = inv.id;
END
$$;
-- +goose StatementEnd
