-- Saved places for mileage claims (PRD-001 FR-AP-16; p1-expense-claims task 4.1, design D5).
--
-- A member keeps the places they often drive between, such as the office and a client's, by name and address, and
-- picks them on a mileage line instead of typing the address again. A place is its person's own: the API lists and
-- changes only the caller's. The address never leaves Fyniq (the maps service stays off). A place keeps its person,
-- changes one version at a time and is never deleted: it is put out of use instead (FY234).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.claim_place (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  user_id         uuid        NOT NULL,
  name            text        NOT NULL,
  address         text        NOT NULL,
  active          boolean     NOT NULL DEFAULT true,
  created_by      text        NOT NULL,
  created_at      timestamptz NOT NULL,
  changed_by      text        NOT NULL,
  changed_at      timestamptz NOT NULL,
  version         integer     NOT NULL DEFAULT 1,
  CONSTRAINT claim_place_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT claim_place_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT claim_place_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT claim_place_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 80),
  CONSTRAINT claim_place_address_length CHECK (length(btrim(address)) BETWEEN 1 AND 300),
  CONSTRAINT claim_place_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND changed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT claim_place_version_positive CHECK (version >= 1)
);
-- A person's live places have names of their own.
CREATE UNIQUE INDEX claim_place_live_name ON fyniq.claim_place (organisation_id, user_id, lower(btrim(name))) WHERE active;

-- +goose StatementBegin
-- A place keeps its person and creation, changes one version at a time and is never deleted (FY234).
CREATE FUNCTION fyniq.claim_place_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'Saved place % is never deleted', OLD.id USING ERRCODE = 'FY234', HINT = 'Put it out of use instead.';
  END IF;
  IF TG_OP = 'INSERT' AND NEW.version <> 1 THEN
    RAISE EXCEPTION 'A new saved place starts at version 1' USING ERRCODE = 'FY234';
  END IF;
  IF TG_OP = 'UPDATE' AND ((NEW.user_id, NEW.created_by, NEW.created_at) IS DISTINCT FROM (OLD.user_id, OLD.created_by, OLD.created_at)
                           OR NEW.version <> OLD.version + 1) THEN
    RAISE EXCEPTION 'Saved place % keeps its person and creation, one version at a time', OLD.id USING ERRCODE = 'FY234';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER claim_place_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.claim_place
  FOR EACH ROW EXECUTE FUNCTION fyniq.claim_place_guard();

ALTER TABLE fyniq.claim_place ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.claim_place FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.claim_place
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.claim_place
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('claim_place', 'id');

REVOKE ALL ON fyniq.claim_place FROM PUBLIC;
GRANT SELECT ON fyniq.claim_place TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (user_id, name, address, created_by, created_at, changed_by, changed_at) ON fyniq.claim_place TO fyniq_api;
GRANT UPDATE (name, address, active, changed_by, changed_at, version) ON fyniq.claim_place TO fyniq_api;
