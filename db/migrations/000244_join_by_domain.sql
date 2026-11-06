-- Joining by email domain (PRD-001 FR-ADM-08; rev 0.199; p1-users-roles task 2.4), on the owner's decisions of 6 and 7
-- Nov 2026: an Owner claims the organisation's email domains (each domain belongs to one organisation only); a Super
-- admin turns joining by domain on for a provider, for the domains it accepts; a person who signs in through that
-- provider with a verified email on a claimed domain, and has no Fyniq account, may ask to join: a pending request that
-- grants nothing until an Owner approves it, which makes them a member with the Employee role. Both switches must be on.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.sign_in_method ADD COLUMN join_by_domain boolean NOT NULL DEFAULT false;
ALTER TABLE fyniq.sign_in_method ADD CONSTRAINT sign_in_method_join_needs_domains
  CHECK (NOT join_by_domain OR cardinality(allowed_domains) > 0) NOT VALID;

-- The verified email the provider gave at sign-in, for joining by domain: kept on the session, which may have no user yet.
ALTER TABLE fyniq.user_session ADD COLUMN email text;
ALTER TABLE fyniq.user_session ADD CONSTRAINT user_session_email_length CHECK (email IS NULL OR length(email) BETWEEN 3 AND 320) NOT VALID;

-- The email domains each organisation claimed for joining: one organisation per domain. A global row: the organisation
-- is the claimant, not a tenant key.
CREATE TABLE fyniq.join_domain (
  domain                text        NOT NULL,
  claiming_organisation uuid        NOT NULL,
  claimed_by            uuid        NOT NULL,
  claimed_at            timestamptz NOT NULL,
  CONSTRAINT join_domain_pkey PRIMARY KEY (domain),
  CONSTRAINT join_domain_claimed_by_fkey FOREIGN KEY (claimed_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT join_domain_format CHECK (domain ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$')
);
CREATE INDEX join_domain_organisation ON fyniq.join_domain (claiming_organisation);
REVOKE ALL ON fyniq.join_domain FROM PUBLIC;

-- A person's request to join an organisation by their email domain: it grants nothing until an Owner approves it.
CREATE TABLE fyniq.join_request (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  issuer          text        NOT NULL,
  subject         text        NOT NULL,
  provider        text        NOT NULL,
  email           text        NOT NULL,
  display_name    text        NOT NULL,
  requested_at    timestamptz NOT NULL,
  status          text        NOT NULL DEFAULT 'pending',
  decided_by      uuid,
  decided_at      timestamptz,
  user_id         uuid,
  CONSTRAINT join_request_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT join_request_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT join_request_decided_by_fkey FOREIGN KEY (decided_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT join_request_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT join_request_status_known CHECK (status IN ('pending', 'approved', 'rejected')),
  CONSTRAINT join_request_decided_whole CHECK ((status = 'pending') = (decided_by IS NULL) AND (decided_by IS NULL) = (decided_at IS NULL)
    AND (status = 'approved') = (user_id IS NOT NULL)),
  CONSTRAINT join_request_name_length CHECK (length(display_name) BETWEEN 1 AND 200),
  CONSTRAINT join_request_email_length CHECK (length(email) BETWEEN 3 AND 320)
);
CREATE UNIQUE INDEX join_request_one_pending ON fyniq.join_request (organisation_id, issuer, subject) WHERE status = 'pending';
ALTER TABLE fyniq.join_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.join_request FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.join_request
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.join_request
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('join_request', 'id');
GRANT SELECT ON fyniq.join_request TO fyniq_api, fyniq_worker, fyniq_readonly;

-- The switches now carry joining by domain: the old set_sign_in_method goes (000240).
DROP FUNCTION fyniq.set_sign_in_method(uuid, uuid, text, integer, boolean, text[], boolean, timestamptz);

-- +goose StatementBegin
-- An active Super admin sets provider p_provider's switches, at version p_version (0: never set), as approved by
-- p_change when two Super admins must agree (FY213; FY273 at another version); turning it off ends its sessions when
-- p_end_sessions; p_join lets people with a verified email on one of its domains ask to join (only with domains).
-- Refused for a domain that is not one, or when an active Super admin would be left with no sign-in that can sign in
-- (FY272). Returns the row.
CREATE FUNCTION fyniq.set_sign_in_method(p_by uuid, p_change uuid, p_provider text, p_version integer, p_enabled boolean,
  p_domains text[], p_join boolean, p_end_sessions boolean, p_at timestamptz) RETURNS fyniq.sign_in_method
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  cur fyniq.sign_in_method;
  out fyniq.sign_in_method;
  d   text;
BEGIN
  PERFORM fyniq.require_super_admin(p_by);
  PERFORM fyniq.apply_installation_change(p_change, 'sign-in:' || p_provider, NULL, 'sign_in', p_at);
  FOREACH d IN ARRAY coalesce(p_domains, '{}') LOOP
    IF d !~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$' THEN
      RAISE EXCEPTION 'Domain % is not a lower-case domain name', d USING ERRCODE = 'FY272',
        HINT = 'Give each domain as people''s email addresses end, such as example.com.';
    END IF;
  END LOOP;
  IF p_join AND cardinality(coalesce(p_domains, '{}')) = 0 THEN
    RAISE EXCEPTION 'Joining by domain needs the domains it is for' USING ERRCODE = 'FY272',
      HINT = 'Set the email domains the provider accepts first.';
  END IF;
  SELECT * INTO cur FROM fyniq.sign_in_method WHERE provider = p_provider FOR UPDATE;
  IF coalesce(cur.version, 0) <> p_version THEN
    RAISE EXCEPTION 'Provider % changed since version %', p_provider, p_version USING ERRCODE = 'FY273',
      HINT = 'Read the provider again and make the change on what it is now.';
  END IF;
  INSERT INTO fyniq.sign_in_method AS m (provider, enabled, allowed_domains, join_by_domain, version, changed_by, changed_at, change_id)
  VALUES (p_provider, p_enabled, coalesce(p_domains, '{}'), p_join, 1, p_by, p_at, p_change)
  ON CONFLICT (provider) DO UPDATE SET enabled = excluded.enabled, allowed_domains = excluded.allowed_domains,
    join_by_domain = excluded.join_by_domain, version = m.version + 1, changed_by = excluded.changed_by, changed_at = excluded.changed_at,
    change_id = excluded.change_id
  RETURNING * INTO out;
  IF EXISTS (SELECT 1 FROM fyniq.super_admin a JOIN fyniq.app_user u USING (user_id)
              WHERE a.removed_at IS NULL AND u.status = 'active'
                AND NOT EXISTS (SELECT 1 FROM fyniq.user_identity i
                                 WHERE i.user_id = a.user_id AND coalesce(fyniq.identity_may_sign_in(i.issuer, i.subject), false))) THEN
    RAISE EXCEPTION 'That leaves a Super admin no way to sign in' USING ERRCODE = 'FY272',
      HINT = 'Keep on a provider, and a domain, that each Super admin signs in with, or have them link another sign-in first.';
  END IF;
  IF NOT p_enabled AND p_end_sessions THEN
    UPDATE fyniq.user_session s SET ended_at = p_at WHERE s.provider = p_provider AND s.ended_at IS NULL;
  END IF;
  RETURN out;
END
$$;

-- Keep the verified email the provider gave a live session at sign-in.
CREATE FUNCTION fyniq.set_session_email(p_hash bytea, p_email text) RETURNS void
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  UPDATE fyniq.user_session SET email = lower(p_email) WHERE token_hash = p_hash AND ended_at IS NULL
$$;

-- The organisation a live session without a Fyniq user may ask to join: its verified email's domain is one an
-- organisation claimed, and the provider it signed in with is on, accepts that domain and lets people join by it. No row
-- when there is none.
CREATE FUNCTION fyniq.join_offer(p_session bytea, p_now timestamptz) RETURNS TABLE (organisation_id uuid, domain text, email text, provider text)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT j.claiming_organisation, j.domain, s.email, s.provider
    FROM fyniq.user_session s
    JOIN fyniq.join_domain j ON j.domain = split_part(s.email, '@', 2)
    JOIN fyniq.sign_in_method m ON m.provider = s.provider
   WHERE s.token_hash = p_session AND s.ended_at IS NULL AND s.expires_at > p_now AND s.email IS NOT NULL
     AND fyniq.user_for_identity(s.issuer, s.subject) IS NULL
     AND m.enabled AND m.join_by_domain AND j.domain = ANY (m.allowed_domains)
$$;

-- The person signed in to live session p_session asks to join the organisation join_offer gives them, as p_name: a
-- pending request (FY280 without an offer, or with one pending already). Returns the organisation and the request; the
-- tenant is left set to the organisation, whose trail the API writes in the same transaction.
CREATE FUNCTION fyniq.request_to_join(p_session bytea, p_name text, p_now timestamptz, OUT organisation_id uuid, OUT request_id uuid)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  o record;
  s fyniq.user_session;
BEGIN
  SELECT * INTO o FROM fyniq.join_offer(p_session, p_now) LIMIT 1;
  IF o.organisation_id IS NULL THEN
    RAISE EXCEPTION 'No organisation takes a request to join from this sign-in' USING ERRCODE = 'FY280',
      HINT = 'Ask the organisation for an invitation.';
  END IF;
  SELECT * INTO s FROM fyniq.user_session x WHERE x.token_hash = p_session;
  PERFORM set_config('app.org_id', o.organisation_id::text, true);
  IF EXISTS (SELECT 1 FROM fyniq.join_request r WHERE r.organisation_id = o.organisation_id AND r.issuer = s.issuer AND r.subject = s.subject
              AND r.status = 'pending') THEN
    RAISE EXCEPTION 'You asked to join already' USING ERRCODE = 'FY280', HINT = 'An Owner decides it; you are told when they do.';
  END IF;
  INSERT INTO fyniq.join_request (organisation_id, issuer, subject, provider, email, display_name, requested_at)
  VALUES (o.organisation_id, s.issuer, s.subject, s.provider, o.email, p_name, p_now) RETURNING id INTO request_id;
  organisation_id := o.organisation_id;
END
$$;

-- p_by, whom the API found an Owner of the current organisation, approves or rejects its pending join request p_id.
-- Approving makes the person a Fyniq user with the identity they asked from and a member with the Employee role
-- (FY280 when that identity has an account by now). Returns the user (none for a rejection) and the membership.
CREATE FUNCTION fyniq.decide_join_request(p_by uuid, p_id uuid, p_approve boolean, p_now timestamptz, OUT user_id uuid,
  OUT membership_id uuid, OUT role_assignment_id uuid)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  r   fyniq.join_request;
  org uuid := fyniq.current_org();
BEGIN
  SELECT * INTO r FROM fyniq.join_request x WHERE x.organisation_id = org AND x.id = p_id AND x.status = 'pending' FOR UPDATE;
  IF r.id IS NULL THEN
    RAISE EXCEPTION 'No pending request %', p_id USING ERRCODE = 'P0002';
  END IF;
  IF NOT p_approve THEN
    UPDATE fyniq.join_request x SET status = 'rejected', decided_by = p_by, decided_at = p_now WHERE x.organisation_id = org AND x.id = p_id;
    RETURN;
  END IF;
  IF fyniq.user_for_identity(r.issuer, r.subject) IS NOT NULL THEN
    RAISE EXCEPTION 'The person has a Fyniq account by now' USING ERRCODE = 'FY280',
      HINT = 'Reject the request, and invite them instead.';
  END IF;
  INSERT INTO fyniq.app_user (display_name) VALUES (r.display_name) RETURNING app_user.user_id INTO decide_join_request.user_id;
  INSERT INTO fyniq.user_identity (issuer, subject, user_id, provider, email, email_verified_at)
  VALUES (r.issuer, r.subject, decide_join_request.user_id, r.provider, r.email, r.requested_at);
  PERFORM pg_advisory_xact_lock(hashtextextended('fyniq.members:' || org::text, 0));
  INSERT INTO fyniq.user_membership (user_id) VALUES (decide_join_request.user_id) RETURNING id INTO membership_id;
  INSERT INTO fyniq.role_assignment (membership_id, role) VALUES (membership_id, 'employee') RETURNING id INTO role_assignment_id;
  UPDATE fyniq.join_request x SET status = 'approved', decided_by = p_by, decided_at = p_now, user_id = decide_join_request.user_id
   WHERE x.organisation_id = org AND x.id = p_id;
END
$$;

-- An Owner of p_org claims email domain p_domain for joining (FY281 when another organisation has it).
CREATE FUNCTION fyniq.claim_join_domain(p_by uuid, p_org uuid, p_domain text, p_now timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM fyniq.join_domain j WHERE j.domain = p_domain AND j.claiming_organisation <> p_org) THEN
    RAISE EXCEPTION 'Another organisation claimed % already', p_domain USING ERRCODE = 'FY281',
      HINT = 'A domain joins one organisation only: ask a Super admin if it is yours.';
  END IF;
  INSERT INTO fyniq.join_domain (domain, claiming_organisation, claimed_by, claimed_at) VALUES (p_domain, p_org, p_by, p_now)
  ON CONFLICT (domain) DO NOTHING;
END
$$;

-- An Owner of p_org releases its claim on p_domain; false when it had none.
CREATE FUNCTION fyniq.release_join_domain(p_org uuid, p_domain text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  DELETE FROM fyniq.join_domain j WHERE j.domain = p_domain AND j.claiming_organisation = p_org;
  RETURN FOUND;
END
$$;

-- The email domains p_org claimed, with when and by whom.
CREATE FUNCTION fyniq.join_domains(p_org uuid) RETURNS SETOF fyniq.join_domain
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT * FROM fyniq.join_domain j WHERE j.claiming_organisation = p_org ORDER BY j.domain
$$;

-- The name of the organisation a join offer names, for the person deciding whether to ask.
CREATE FUNCTION fyniq.join_offer_name(p_org uuid) RETURNS text
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  n text;
BEGIN
  PERFORM set_config('app.org_id', p_org::text, true);
  SELECT o.name INTO n FROM fyniq.organisation o WHERE o.organisation_id = p_org;
  PERFORM set_config('app.org_id', '', true);
  RETURN n;
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.set_sign_in_method(uuid, uuid, text, integer, boolean, text[], boolean, boolean, timestamptz),
  fyniq.set_session_email(bytea, text), fyniq.join_offer(bytea, timestamptz), fyniq.request_to_join(bytea, text, timestamptz),
  fyniq.decide_join_request(uuid, uuid, boolean, timestamptz), fyniq.claim_join_domain(uuid, uuid, text, timestamptz),
  fyniq.release_join_domain(uuid, text), fyniq.join_domains(uuid), fyniq.join_offer_name(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.set_sign_in_method(uuid, uuid, text, integer, boolean, text[], boolean, boolean, timestamptz),
  fyniq.set_session_email(bytea, text), fyniq.join_offer(bytea, timestamptz), fyniq.request_to_join(bytea, text, timestamptz),
  fyniq.decide_join_request(uuid, uuid, boolean, timestamptz), fyniq.claim_join_domain(uuid, uuid, text, timestamptz),
  fyniq.release_join_domain(uuid, text), fyniq.join_domains(uuid), fyniq.join_offer_name(uuid) TO fyniq_api;
