-- Roles, schemas and extensions (PRD-001 §11.3, §11.4; SEC-03).
--
-- Runs as fyniq_migrator, the only role that owns objects. The environment creates fyniq_migrator
-- before the first run (locally `migrate up` in the local build; in the cloud p0-infra-opentofu).
-- The roles below are NOLOGIN group roles: login identities and passwords never appear in a
-- migration. Each environment grants these roles to its own service identities.
--
-- Every role is closed by default: nothing here grants a table. Later migrations grant each role
-- exactly what its service needs; the catalogue test (SEC-03) compares the grants with the table in
-- db/README.md.

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
    'fyniq_api',       -- the API: domain commands and reads
    'fyniq_worker',    -- background work: outbox delivery, scheduled jobs, nightly verification
    'fyniq_executor',  -- the proposal executor: approved proposals only (§9.6)
    'fyniq_ai',        -- the AI orchestrator: no write grant, ever (AI-P2, CMP-AI-01)
    'fyniq_readonly'   -- read-only access for investigations and reports
  ] LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = r) THEN
      EXECUTE format(
        'CREATE ROLE %I NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS', r);
    END IF;
  END LOOP;
END
$$;
-- +goose StatementEnd

-- Application objects. Only fyniq_migrator owns them, and no service connects as fyniq_migrator.
CREATE SCHEMA fyniq;
REVOKE ALL ON SCHEMA fyniq FROM PUBLIC;
GRANT USAGE ON SCHEMA fyniq TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_ai, fyniq_readonly;

-- Functions fyniq_migrator creates are not executable by PUBLIC; each grant is explicit. (A
-- per-schema default cannot revoke the global one, so this is the global default for the role.)
ALTER DEFAULT PRIVILEGES REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;

-- Extensions live in their own schema, away from the application's objects. Both are trusted
-- extensions, available on Cloud SQL, AlloyDB, RDS, Aurora and Azure (NFR-PORT-02). pg_cron,
-- pgaudit and pg_partman are instance settings, enabled by p0-infra-opentofu.
CREATE SCHEMA fyniq_ext;
REVOKE ALL ON SCHEMA fyniq_ext FROM PUBLIC;
CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA fyniq_ext;    -- the audit chain (§11.5)
CREATE EXTENSION IF NOT EXISTS btree_gist WITH SCHEMA fyniq_ext;  -- non-overlapping periods
