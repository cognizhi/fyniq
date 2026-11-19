-- Nemura's roles, schema and tenancy base (PRD-002 ADR-N1, NM-ARC-02; PRD-001 §11.4, SEC-03; golden rules 2 and 8).
--
-- Runs as nemura_migrator, the only role that owns objects, in the database `nemura`: a database of its own, never
-- Fyniq's. The environment creates nemura_migrator and the database before the first run (locally the `migrate` role of
-- the nemura image in the local build; in the cloud infra/). Roles are cluster-wide in PostgreSQL, so every role here is
-- named nemura_*, and none is shared with Fyniq's fyniq_*. The roles are NOLOGIN group roles: login identities and
-- passwords never appear in a migration.
--
-- Every role is closed by default: nothing here grants a table beyond the organisation's own row. Later migrations
-- grant each role exactly what its service needs; the grants golden file (testdata/grants.golden) fails on any
-- difference (SEC-03).
--
-- Tenancy rules every tenant table follows (the same as Fyniq's, INV-06): organisation_id on every row, first in every
-- index and primary key; UNIQUE (organisation_id, id); composite foreign keys between tenant tables, because foreign-key
-- checks bypass row-level security; ENABLE and FORCE ROW LEVEL SECURITY with one policy on nemura.current_org().

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
DO $$
DECLARE
  r text;
BEGIN
  FOREACH r IN ARRAY ARRAY[
    'nemura_api',        -- the API: domain commands and reads
    'nemura_worker',     -- background work: outbox delivery, scheduled jobs, nightly verification
    'nemura_executor',   -- the proposal executor: approved proposals only (NM-ARC-06)
    'nemura_assistant',  -- the people assistant's runtime: inserts proposals only, when they exist (NM-ARC-06)
    'nemura_readonly'    -- read-only access for investigations and reports
  ] LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = r) THEN
      EXECUTE format(
        'CREATE ROLE %I NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS', r);
    END IF;
  END LOOP;
END
$$;
-- +goose StatementEnd

-- This database is Nemura's alone: nobody connects unless they hold a Nemura role (a Fyniq role, a person or a
-- stray login cannot even open it), and PUBLIC holds nothing on it, not even temporary tables. The migrator owns it.
-- +goose StatementBegin
DO $$
BEGIN
  EXECUTE format('REVOKE ALL ON DATABASE %I FROM PUBLIC', current_database());
  EXECUTE format('GRANT CONNECT ON DATABASE %I TO nemura_api, nemura_worker, nemura_executor, nemura_assistant, nemura_readonly',
                 current_database());
END
$$;
-- +goose StatementEnd

-- Application objects. Only nemura_migrator owns them, and no service connects as nemura_migrator.
CREATE SCHEMA nemura;
REVOKE ALL ON SCHEMA nemura FROM PUBLIC;
GRANT USAGE ON SCHEMA nemura TO nemura_api, nemura_worker, nemura_executor, nemura_assistant, nemura_readonly;

-- Functions nemura_migrator creates are not executable by PUBLIC; each grant is explicit.
ALTER DEFAULT PRIVILEGES REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;

-- The organisation of the current transaction. The unit of work sets it first, transaction-scoped:
-- set_config('app.org_id', <id>, true). Unset, or reset to '' after an earlier transaction on a pooled connection, it is
-- NULL and matches no row. A value that is not a UUID raises an error.
-- +goose StatementBegin
CREATE FUNCTION nemura.current_org() RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE
AS $$
  SELECT CAST(nullif(pg_catalog.current_setting('app.org_id', true), '') AS pg_catalog.uuid)
$$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION nemura.current_org() TO nemura_api, nemura_worker, nemura_executor, nemura_assistant, nemura_readonly;

-- The tenant as a value that must be there: for triggers that write on the tenant's behalf.
-- +goose StatementBegin
CREATE FUNCTION nemura.require_org() RETURNS uuid
LANGUAGE plpgsql STABLE
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  org uuid := nemura.current_org();
BEGIN
  IF org IS NULL THEN
    RAISE EXCEPTION 'No organisation is set for this transaction'
      USING ERRCODE = 'NM006',
            HINT = 'Run the work through the unit of work, which sets app.org_id first (INV-06).';
  END IF;
  RETURN org;
END
$$;
-- +goose StatementEnd

-- ------------------------------------------------------------------ tenant: organisations
-- A company's Nemura workspace. The organisation's own row is its tenancy: the unit of work sets app.org_id to a new
-- id and the row takes it as its id.
CREATE TABLE nemura.organisation (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  name            text        NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT organisation_pkey PRIMARY KEY (organisation_id),
  CONSTRAINT organisation_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 200)
);

ALTER TABLE nemura.organisation ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.organisation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.organisation
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.organisation TO nemura_api, nemura_worker, nemura_executor, nemura_readonly;
GRANT INSERT (name) ON nemura.organisation TO nemura_api;

-- ------------------------------------------------------------------ global: the tenant directory
-- The organisations that exist, for jobs that run for every organisation (nightly verification, anchoring). Only ids: a
-- global table with no grant to any application role, read by the worker through the tenants view.
CREATE TABLE nemura.tenant_directory (
  organisation_id uuid        NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT tenant_directory_pkey PRIMARY KEY (organisation_id),
  CONSTRAINT tenant_directory_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id)
);
CREATE VIEW nemura.tenants AS SELECT organisation_id, created_at FROM nemura.tenant_directory;
GRANT SELECT ON nemura.tenants TO nemura_worker;

-- +goose StatementBegin
CREATE FUNCTION nemura.organisation_registered() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  INSERT INTO nemura.tenant_directory (organisation_id) VALUES (NEW.organisation_id);
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER organisation_registered AFTER INSERT ON nemura.organisation
  FOR EACH ROW EXECUTE FUNCTION nemura.organisation_registered();
