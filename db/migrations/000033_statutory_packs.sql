-- Statutory parameter packs (PRD-001 §10.6, ENG-30, FR-ORG-05; p1-statutory-packs task 1.2).
--
-- A pack version is data in git (packs/<pack>/<year>.<n>/), validated by internal/domain/pack and copied
-- here by the migrator when it deploys a release (`migrate up`): the release is the audited write path
-- (CTL-CHG-01). Both tables are global (§11.4): no organisation_id and no grant to any application role;
-- the services read them through the two views. A loaded version never changes and is never removed: a
-- correction is a new version, and the migrator refuses to load a version again with other contents
-- (FY110). Each organisation's activation of a version is its own tenant row (000034).

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.statutory_pack_version (
  id             text        NOT NULL,
  pack           text        NOT NULL,
  title          text        NOT NULL,
  effective_from date        NOT NULL,
  effective_to   date,
  supersedes     text,
  review_date    date        NOT NULL,
  verifier       text        NOT NULL,
  conditions     text        NOT NULL,
  tax_point_rule text,
  sources        jsonb       NOT NULL,
  digest         text        NOT NULL,
  loaded_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT statutory_pack_version_pkey PRIMARY KEY (id),
  CONSTRAINT statutory_pack_version_id_format CHECK (id ~ '^[a-z][a-z0-9-]{1,30}@20[0-9]{2}\.[1-9][0-9]{0,2}$'),
  CONSTRAINT statutory_pack_version_pack_matches CHECK (id LIKE pack || '@%'),
  CONSTRAINT statutory_pack_version_dates CHECK (effective_to IS NULL OR effective_to >= effective_from),
  CONSTRAINT statutory_pack_version_verifier CHECK (length(btrim(verifier)) BETWEEN 1 AND 300),
  CONSTRAINT statutory_pack_version_title_length CHECK (length(title) BETWEEN 1 AND 300),
  CONSTRAINT statutory_pack_version_conditions_length CHECK (length(conditions) BETWEEN 1 AND 2000),
  -- ENG-30: every version cites at least one source, each with an https URL.
  CONSTRAINT statutory_pack_version_sources CHECK (jsonb_typeof(sources) = 'array' AND jsonb_array_length(sources) > 0),
  -- A value still marked [verify] never reaches the database (the loader refuses it too).
  CONSTRAINT statutory_pack_version_no_verify CHECK (sources::text !~* '\[\s*verify' AND verifier !~* '\[\s*verify'),
  CONSTRAINT statutory_pack_version_digest_format CHECK (digest ~ '^[0-9a-f]{64}$')
);

CREATE TABLE fyniq.statutory_pack_row (
  version_id text    NOT NULL,
  table_name text    NOT NULL,
  pinned     boolean NOT NULL,
  row_no     integer NOT NULL,
  cells      jsonb   NOT NULL,
  CONSTRAINT statutory_pack_row_pkey PRIMARY KEY (version_id, pinned, table_name, row_no),
  CONSTRAINT statutory_pack_row_version_fkey FOREIGN KEY (version_id) REFERENCES fyniq.statutory_pack_version (id),
  CONSTRAINT statutory_pack_row_table_format CHECK (table_name ~ '^[a-z][a-z0-9_]{0,40}$'),
  CONSTRAINT statutory_pack_row_row_no_positive CHECK (row_no > 0),
  -- Cells are strings: decimals stay exact decimal strings, never floats (ENG-10).
  CONSTRAINT statutory_pack_row_cells_object CHECK (jsonb_typeof(cells) = 'object'),
  CONSTRAINT statutory_pack_row_no_verify CHECK (cells::text !~* '\[\s*verify')
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.statutory_pack_immutable() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A loaded statutory pack version never changes and is never removed'
    USING ERRCODE = 'FY110',
          HINT = 'Publish a correction as a new version, <pack>@<year>.<n+1> (ENG-30, ENG-31).';
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.statutory_pack_immutable() FROM PUBLIC;

CREATE TRIGGER statutory_pack_version_immutable BEFORE UPDATE OR DELETE ON fyniq.statutory_pack_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.statutory_pack_immutable();
CREATE TRIGGER statutory_pack_row_immutable BEFORE UPDATE OR DELETE ON fyniq.statutory_pack_row
  FOR EACH ROW EXECUTE FUNCTION fyniq.statutory_pack_immutable();

CREATE VIEW fyniq.statutory_pack_versions AS
  SELECT id, pack, title, effective_from, effective_to, supersedes, review_date, verifier, conditions,
         tax_point_rule, sources, digest, loaded_at
    FROM fyniq.statutory_pack_version;
CREATE VIEW fyniq.statutory_pack_rows AS
  SELECT version_id, table_name, pinned, row_no, cells FROM fyniq.statutory_pack_row;
GRANT SELECT ON fyniq.statutory_pack_versions, fyniq.statutory_pack_rows
  TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
