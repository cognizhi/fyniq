-- Switching sign-in providers (PRD-001 FR-ADM-08; rev 0.197; p1-users-roles task 2.4): the providers are set in the
-- deployment's configuration (the owner's decision of 6 Nov 2026); here a Super admin turns each on or off, optionally
-- ending its sessions, and sets the email domains it accepts. A provider with no row is on and accepts any domain.
-- The platform refuses a change that leaves an active Super admin no way to sign in (FY272). With two Super admins a
-- change waits for the other's approval, as an installation change of step sign_in (FR-ADM-07). The API writes each
-- change to the installation's trail, in the same transaction.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.sign_in_method (
  provider        text        NOT NULL,
  enabled         boolean     NOT NULL DEFAULT true,
  allowed_domains text[]      NOT NULL DEFAULT '{}',
  version         integer     NOT NULL DEFAULT 1,
  changed_by      uuid        NOT NULL,
  changed_at      timestamptz NOT NULL,
  change_id       uuid,
  CONSTRAINT sign_in_method_pkey PRIMARY KEY (provider),
  CONSTRAINT sign_in_method_changed_by_fkey FOREIGN KEY (changed_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT sign_in_method_change_fkey FOREIGN KEY (change_id) REFERENCES fyniq.installation_change (id),
  CONSTRAINT sign_in_method_provider_format CHECK (provider ~ '^[a-z0-9][a-z0-9-]{0,62}$'),
  CONSTRAINT sign_in_method_domains_count CHECK (cardinality(allowed_domains) <= 20),
  CONSTRAINT sign_in_method_version_positive CHECK (version >= 1)
);
REVOKE ALL ON fyniq.sign_in_method FROM PUBLIC;

ALTER TABLE fyniq.installation_change DROP CONSTRAINT installation_change_step_known;
ALTER TABLE fyniq.installation_change ADD CONSTRAINT installation_change_step_known
  CHECK (step IN ('install', 'configure', 'secret', 'enable', 'resume', 'disable', 'uninstall', 'order', 'consent', 'sign_in')) NOT VALID;

-- +goose StatementBegin
-- The providers' switches, by provider.
CREATE FUNCTION fyniq.sign_in_methods() RETURNS SETOF fyniq.sign_in_method
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT * FROM fyniq.sign_in_method ORDER BY provider
$$;

-- How many sessions each provider started since p_since.
CREATE FUNCTION fyniq.sign_in_counts(p_since timestamptz) RETURNS TABLE (provider text, sign_ins bigint)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT s.provider, count(*) FROM fyniq.user_session s WHERE s.created_at >= p_since GROUP BY s.provider ORDER BY s.provider
$$;

-- Whether a sign-in identity can sign in under the switches as they are: its provider (recorded when linked, else its
-- last session's) is on, and, when the provider accepts only some domains, its verified email is on one of them.
CREATE FUNCTION fyniq.identity_may_sign_in(p_issuer text, p_subject text) RETURNS boolean
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT coalesce(m.enabled, true)
     AND (m.provider IS NULL OR cardinality(m.allowed_domains) = 0
          OR (i.email_verified_at IS NOT NULL AND lower(split_part(i.email, '@', 2)) = ANY (m.allowed_domains)))
    FROM fyniq.user_identity i
    LEFT JOIN fyniq.sign_in_method m
      ON m.provider = coalesce(i.provider, (SELECT s.provider FROM fyniq.user_session s WHERE s.issuer = i.issuer AND s.subject = i.subject
                                            ORDER BY s.created_at DESC LIMIT 1))
   WHERE i.issuer = p_issuer AND i.subject = p_subject
     AND coalesce(i.provider, (SELECT s.provider FROM fyniq.user_session s WHERE s.issuer = i.issuer AND s.subject = i.subject
                               ORDER BY s.created_at DESC LIMIT 1)) IS NOT NULL
$$;

-- The providers that some active Super admin signs in with alone, under the switches as they are: turning one off, or
-- narrowing its domains past their email, would leave them no way in.
CREATE FUNCTION fyniq.sign_in_essential() RETURNS SETOF text
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  WITH usable AS (
    SELECT i.user_id, coalesce(i.provider, (SELECT s.provider FROM fyniq.user_session s WHERE s.issuer = i.issuer AND s.subject = i.subject
                                            ORDER BY s.created_at DESC LIMIT 1)) AS provider
      FROM fyniq.user_identity i
      JOIN fyniq.super_admin a ON a.user_id = i.user_id AND a.removed_at IS NULL
      JOIN fyniq.app_user u ON u.user_id = i.user_id AND u.status = 'active'
     WHERE coalesce(fyniq.identity_may_sign_in(i.issuer, i.subject), false))
  SELECT DISTINCT x.provider FROM usable x
   WHERE NOT EXISTS (SELECT 1 FROM usable y WHERE y.user_id = x.user_id AND y.provider <> x.provider)
   ORDER BY 1
$$;

-- An active Super admin sets provider p_provider's switches, at version p_version (0: never set), as approved by
-- p_change when two Super admins must agree (FY213; FY273 at another version); turning it off ends its sessions when
-- p_end_sessions. Refused for a domain that is not one, or when an active Super admin would be left with no sign-in
-- that can sign in (FY272). Returns the row.
CREATE FUNCTION fyniq.set_sign_in_method(p_by uuid, p_change uuid, p_provider text, p_version integer, p_enabled boolean,
  p_domains text[], p_end_sessions boolean, p_at timestamptz) RETURNS fyniq.sign_in_method
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
  SELECT * INTO cur FROM fyniq.sign_in_method WHERE provider = p_provider FOR UPDATE;
  IF coalesce(cur.version, 0) <> p_version THEN
    RAISE EXCEPTION 'Provider % changed since version %', p_provider, p_version USING ERRCODE = 'FY273',
      HINT = 'Read the provider again and make the change on what it is now.';
  END IF;
  INSERT INTO fyniq.sign_in_method AS m (provider, enabled, allowed_domains, version, changed_by, changed_at, change_id)
  VALUES (p_provider, p_enabled, coalesce(p_domains, '{}'), 1, p_by, p_at, p_change)
  ON CONFLICT (provider) DO UPDATE SET enabled = excluded.enabled, allowed_domains = excluded.allowed_domains, version = m.version + 1,
    changed_by = excluded.changed_by, changed_at = excluded.changed_at, change_id = excluded.change_id
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
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.sign_in_methods(), fyniq.sign_in_counts(timestamptz), fyniq.identity_may_sign_in(text, text),
  fyniq.sign_in_essential(),
  fyniq.set_sign_in_method(uuid, uuid, text, integer, boolean, text[], boolean, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.sign_in_methods(), fyniq.sign_in_counts(timestamptz), fyniq.sign_in_essential(),
  fyniq.set_sign_in_method(uuid, uuid, text, integer, boolean, text[], boolean, timestamptz) TO fyniq_api;
