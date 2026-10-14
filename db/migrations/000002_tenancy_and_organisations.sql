-- Tenancy, currencies, organisations, the ledger revision and idempotency keys
-- (PRD-001 §11.3, §11.4; INV-06, SEC-02, ENG-46, FR-INT-08).
--
-- Tenancy rules every tenant table follows (checked by the catalogue test, INV-06):
--   * organisation_id on every row, the first column of every index and of the primary key;
--   * UNIQUE (organisation_id, id), and every foreign key between tenant tables is composite,
--     because foreign-key and unique checks bypass row-level security;
--   * ENABLE and FORCE ROW LEVEL SECURITY, with one policy that matches fyniq.current_org();
--   * no TRUNCATE, REFERENCES, TRIGGER or ownership for any application role.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- The organisation of the current transaction. The unit of work sets it first, transaction-scoped:
-- set_config('app.org_id', <id>, true). Unset, or reset to '' after an earlier transaction on a
-- pooled connection, it is NULL and matches no row. A value that is not a UUID raises an error.
-- +goose StatementBegin
CREATE FUNCTION fyniq.current_org() RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE
AS $$
  SELECT CAST(nullif(pg_catalog.current_setting('app.org_id', true), '') AS pg_catalog.uuid)
$$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION fyniq.current_org() TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;

-- The tenant as a value that must be there: for triggers that write on the tenant's behalf.
-- +goose StatementBegin
CREATE FUNCTION fyniq.require_org() RETURNS uuid
LANGUAGE plpgsql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.current_org();
BEGIN
  IF org IS NULL THEN
    RAISE EXCEPTION 'No organisation is set for this transaction'
      USING ERRCODE = 'FY006',
            HINT = 'Run the work through the unit of work, which sets app.org_id first (INV-06).';
  END IF;
  RETURN org;
END
$$;
-- +goose StatementEnd

-- ------------------------------------------------------------------ global: currencies
-- ISO 4217 codes and minor units, from the ISO 4217 list maintained by SIX
-- (https://www.six-group.com/en/products-services/financial-information/data-standards.html).
-- A global table: no organisation_id, no grant to any application role; services read it through
-- the fyniq.currencies view (§11.4, global tables).
CREATE TABLE fyniq.currency (
  code        text     NOT NULL,
  minor_units smallint NOT NULL,
  name        text     NOT NULL,
  CONSTRAINT currency_pkey PRIMARY KEY (code),
  CONSTRAINT currency_code_minor_units_key UNIQUE (code, minor_units),
  CONSTRAINT currency_code_format CHECK (code ~ '^[A-Z]{3}$'),
  -- NUMERIC(20,4) holds at most four decimals
  CONSTRAINT currency_minor_units_range CHECK (minor_units BETWEEN 0 AND 4),
  CONSTRAINT currency_name_length CHECK (length(name) BETWEEN 1 AND 100)
);

INSERT INTO fyniq.currency (code, minor_units, name) VALUES
  ('AUD', 2, 'Australian dollar'),
  ('BHD', 3, 'Bahraini dinar'),
  ('CAD', 2, 'Canadian dollar'),
  ('CHF', 2, 'Swiss franc'),
  ('CNY', 2, 'Yuan renminbi'),
  ('EUR', 2, 'Euro'),
  ('GBP', 2, 'Pound sterling'),
  ('HKD', 2, 'Hong Kong dollar'),
  ('IDR', 2, 'Rupiah'),
  ('INR', 2, 'Indian rupee'),
  ('JPY', 0, 'Yen'),
  ('KRW', 0, 'Won'),
  ('KWD', 3, 'Kuwaiti dinar'),
  ('MYR', 2, 'Malaysian ringgit'),
  ('NZD', 2, 'New Zealand dollar'),
  ('PHP', 2, 'Philippine peso'),
  ('SGD', 2, 'Singapore dollar'),
  ('THB', 2, 'Baht'),
  ('TWD', 2, 'New Taiwan dollar'),
  ('USD', 2, 'US dollar'),
  ('VND', 0, 'Dong');

CREATE VIEW fyniq.currencies AS
  SELECT code, minor_units, name FROM fyniq.currency;
GRANT SELECT ON fyniq.currencies TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;

-- ------------------------------------------------------------------ tenant: organisations
CREATE TABLE fyniq.organisation (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  name             text        NOT NULL,
  base_currency    text        NOT NULL,
  base_minor_units smallint    NOT NULL,
  version          integer     NOT NULL DEFAULT 1,
  created_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT organisation_pkey PRIMARY KEY (organisation_id),
  CONSTRAINT organisation_base_currency_fkey FOREIGN KEY (base_currency, base_minor_units)
    REFERENCES fyniq.currency (code, minor_units),
  CONSTRAINT organisation_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 200),
  CONSTRAINT organisation_version_positive CHECK (version > 0)
);

-- The organisation's ledger revision (ENG-46): one row per organisation, created with it. Every
-- journal entry takes the next revision under this row's lock, held until commit, so revisions
-- become visible in order and an aborted posting leaves no gap. Only the ledger's own functions
-- change it; no application role has a write grant.
CREATE TABLE fyniq.ledger_revision (
  organisation_id uuid   NOT NULL DEFAULT fyniq.current_org(),
  revision        bigint NOT NULL DEFAULT 0,
  CONSTRAINT ledger_revision_pkey PRIMARY KEY (organisation_id),
  CONSTRAINT ledger_revision_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT ledger_revision_not_negative CHECK (revision >= 0)
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.organisation_created() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  INSERT INTO fyniq.ledger_revision (organisation_id, revision) VALUES (NEW.organisation_id, 0);
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER organisation_created AFTER INSERT ON fyniq.organisation
  FOR EACH ROW EXECUTE FUNCTION fyniq.organisation_created();

-- The base currency never changes: every base amount was converted into it (ENG-14).
-- +goose StatementBegin
CREATE FUNCTION fyniq.organisation_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.base_currency IS DISTINCT FROM OLD.base_currency
     OR NEW.base_minor_units IS DISTINCT FROM OLD.base_minor_units THEN
    RAISE EXCEPTION 'The base currency of an organisation cannot change'
      USING ERRCODE = 'FY014',
            HINT = 'Base amounts were converted into it when they were posted (ENG-14).';
  END IF;
  IF NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION 'created_at cannot change' USING ERRCODE = 'FY014';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER organisation_guard BEFORE UPDATE ON fyniq.organisation
  FOR EACH ROW EXECUTE FUNCTION fyniq.organisation_guard();

-- The revision only moves forward, one at a time, and its row is never deleted.
-- +goose StatementBegin
CREATE FUNCTION fyniq.ledger_revision_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.revision <> OLD.revision + 1 THEN
    RAISE EXCEPTION 'The ledger revision only increases by one, when a journal entry is posted'
      USING ERRCODE = 'FY046', HINT = 'ENG-46: revisions are gap-free per organisation.';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER ledger_revision_guard BEFORE UPDATE OR DELETE ON fyniq.ledger_revision
  FOR EACH ROW EXECUTE FUNCTION fyniq.ledger_revision_guard();

-- The ledger lock of ENG-47, step 2: takes this organisation's revision row FOR UPDATE and returns
-- the revision. Period close and reopen call it before they lock the period row.
-- +goose StatementBegin
CREATE FUNCTION fyniq.lock_ledger() RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  rev bigint;
BEGIN
  SELECT revision INTO rev FROM fyniq.ledger_revision
   WHERE organisation_id = fyniq.require_org()
   FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'The organisation of this transaction does not exist' USING ERRCODE = 'FY006';
  END IF;
  RETURN rev;
END
$$;
-- +goose StatementEnd

-- ------------------------------------------------------------------ tenant: idempotency keys
-- Transport keys (FR-INT-08): the API's Idempotency-Key, kept 24 hours per client. They only stop a
-- repeated request. The posting identity on journal_entry is the separate key that never expires.
CREATE TABLE fyniq.idempotency_key (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  client_id       text        NOT NULL,
  key             text        NOT NULL,
  request_hash    bytea       NOT NULL,
  response_status smallint,
  response_body   bytea,
  created_at      timestamptz NOT NULL DEFAULT now(),
  expires_at      timestamptz NOT NULL,
  CONSTRAINT idempotency_key_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT idempotency_key_client_key UNIQUE (organisation_id, client_id, key),
  CONSTRAINT idempotency_key_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT idempotency_key_client_length CHECK (length(client_id) BETWEEN 1 AND 200),
  CONSTRAINT idempotency_key_key_length CHECK (length(key) BETWEEN 1 AND 255),
  CONSTRAINT idempotency_key_hash_sha256 CHECK (length(request_hash) = 32),
  CONSTRAINT idempotency_key_status_range CHECK (response_status BETWEEN 100 AND 599),
  CONSTRAINT idempotency_key_expiry CHECK (expires_at > created_at)
);
CREATE INDEX idempotency_key_expires_idx ON fyniq.idempotency_key (organisation_id, expires_at);

-- ------------------------------------------------------------------ row-level security
ALTER TABLE fyniq.organisation ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.organisation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.organisation
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

ALTER TABLE fyniq.ledger_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ledger_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ledger_revision
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

ALTER TABLE fyniq.idempotency_key ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.idempotency_key FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.idempotency_key
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- ------------------------------------------------------------------ grants (db/README.md)
GRANT SELECT ON fyniq.organisation, fyniq.ledger_revision
  TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (name, base_currency, base_minor_units), UPDATE (name, version)
  ON fyniq.organisation TO fyniq_api;
GRANT EXECUTE ON FUNCTION fyniq.lock_ledger() TO fyniq_api, fyniq_worker;

GRANT SELECT ON fyniq.idempotency_key TO fyniq_api, fyniq_readonly;
GRANT INSERT (client_id, key, request_hash, expires_at),
      UPDATE (response_status, response_body)
  ON fyniq.idempotency_key TO fyniq_api;
GRANT SELECT (organisation_id, id, expires_at), DELETE ON fyniq.idempotency_key TO fyniq_worker;
