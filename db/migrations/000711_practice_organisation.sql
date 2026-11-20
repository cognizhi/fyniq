-- A practice organisation (PRD-001 FR-UX-36; the owner's decision of 21 Nov 2026): a separate, clearly labelled organisation an
-- Owner of an existing organisation opens to try a task without touching the real books. ONLY an Owner of an organisation, and ONE
-- AT A TIME: a person who holds a practice organisation opens no other.
--
-- It is made by one security-definer function, owned by the migrator with a fixed search_path, that checks both rules itself and
-- creates the organisation, its membership for the creator and their Owner role, and the registry row that makes the second one
-- impossible (a unique key, so two requests at once cannot both win). The creator reads no other tenant: the function looks only
-- for their own live Owner role and returns three IDs. The caller writes the audit events of both organisations in the same
-- transaction (AUD-01), and the new organisation starts with nothing of the source's: empty books, no copied data.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- The registry: global, like the tenant directory, and read by no application role. Its key says who holds one; the source is kept as
-- an ID only, since a foreign key from a global table to a tenant's row would bypass its row-level security (§11.4).
CREATE TABLE fyniq.practice_organisation (
  organisation_id        uuid        NOT NULL,
  created_by             uuid        NOT NULL,
  source_organisation_id uuid        NOT NULL,
  created_at             timestamptz NOT NULL,
  CONSTRAINT practice_organisation_pkey PRIMARY KEY (organisation_id),
  CONSTRAINT practice_organisation_one_each UNIQUE (created_by),
  CONSTRAINT practice_organisation_org_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT practice_organisation_user_fkey FOREIGN KEY (created_by) REFERENCES fyniq.app_user (user_id)
);
REVOKE ALL ON fyniq.practice_organisation FROM PUBLIC;

-- Whether an organisation is a practice one, for the screens that label it.
-- +goose StatementBegin
CREATE FUNCTION fyniq.organisation_is_practice(p_org uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM fyniq.practice_organisation p WHERE p.organisation_id = p_org)
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION fyniq.create_practice_organisation(p_user uuid, p_source uuid, p_name text, p_currency text, p_at timestamptz,
  OUT organisation_id uuid, OUT membership_id uuid, OUT assignment_id uuid)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  previous text := current_setting('app.org_id', true);
  inst     uuid := fyniq.installation_org();
  is_owner boolean;
  minor    smallint;
BEGIN
  -- One at a time, per person: two requests at once queue here and the second finds the first's row.
  PERFORM pg_advisory_xact_lock(hashtextextended('fyniq.practice:' || p_user::text, 0));
  IF NOT EXISTS (SELECT 1 FROM fyniq.app_user u WHERE u.user_id = p_user AND u.status = 'active') THEN
    RAISE EXCEPTION 'only a member opens a practice organisation' USING ERRCODE = 'FY711';
  END IF;
  IF p_source IS NULL OR p_source = inst OR EXISTS (SELECT 1 FROM fyniq.practice_organisation p WHERE p.organisation_id = p_source) THEN
    RAISE EXCEPTION 'a practice organisation is opened from an organisation that keeps books, never from the installation or another practice organisation'
      USING ERRCODE = 'FY711';
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.practice_organisation p WHERE p.created_by = p_user) THEN
    RAISE EXCEPTION 'you hold a practice organisation already, and hold one at a time' USING ERRCODE = 'FY712',
      HINT = 'Finish with it first: it is deleted through the organisation''s tenant export and delete.';
  END IF;
  -- Their own live Owner role in the source, and nothing else of the source's: this reads one membership of the person asking.
  PERFORM set_config('app.org_id', p_source::text, true);
  SELECT EXISTS (
    SELECT 1 FROM fyniq.user_membership m JOIN fyniq.role_assignment r ON r.organisation_id = m.organisation_id AND r.membership_id = m.id
     WHERE m.organisation_id = p_source AND m.user_id = p_user AND m.status = 'active' AND r.role = 'owner'
       AND r.revoked_at IS NULL AND (r.expires_at IS NULL OR r.expires_at > p_at)) INTO is_owner;
  IF NOT is_owner THEN
    PERFORM set_config('app.org_id', coalesce(previous, ''), true);
    RAISE EXCEPTION 'only an Owner of the organisation opens a practice organisation' USING ERRCODE = 'FY713';
  END IF;
  SELECT c.minor_units INTO minor FROM fyniq.currencies c WHERE c.code = p_currency;
  IF minor IS NULL THEN
    PERFORM set_config('app.org_id', coalesce(previous, ''), true);
    RAISE EXCEPTION 'currency % is not an ISO 4217 code Fyniq knows', p_currency USING ERRCODE = 'FY714';
  END IF;
  -- The new organisation: its row, its creator as a member and its Owner. Nothing is copied from the source.
  organisation_id := uuidv7();
  PERFORM set_config('app.org_id', organisation_id::text, true);
  INSERT INTO fyniq.organisation (name, base_currency, base_minor_units) VALUES (p_name, p_currency, minor);
  INSERT INTO fyniq.user_membership (user_id) VALUES (p_user) RETURNING id INTO membership_id;
  INSERT INTO fyniq.role_assignment (membership_id, role) VALUES (membership_id, 'owner') RETURNING id INTO assignment_id;
  INSERT INTO fyniq.practice_organisation (organisation_id, created_by, source_organisation_id, created_at)
    VALUES (organisation_id, p_user, p_source, p_at);
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.organisation_is_practice(uuid), fyniq.create_practice_organisation(uuid, uuid, text, text, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.organisation_is_practice(uuid), fyniq.create_practice_organisation(uuid, uuid, text, text, timestamptz) TO fyniq_api;
