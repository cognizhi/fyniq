-- Dimensions and dimension sets on journal lines (PRD-001 FR-DIM-01, FR-DIM-02, §11.4; p1-organisation-chart task
-- 3.3, slice 1: the database).
--
-- A dimension is Department, Project or one of up to 8 custom dimensions (FR-DIM-01), each with its values. A
-- dimension set is one combination of values, at most one of each dimension, held once ("dimension_set (one row per
-- distinct combination)", §11.4), and a journal line names its set. A line with no dimension names none: its set is
-- NULL, the default set the design's backfill would have written, so no posted line is rewritten (INV-02; design D,
-- "Dimensions on lines"). A set never changes once written, since posted lines name it.
--
-- The balances cache is keyed by account × period × dimension set × currency (§11.4), NULL being the set of no
-- dimension: 000219 builds that key concurrently, and 000220 moves the posting trigger, the INV-08 rebuild and the
-- comparison of the cache with the lines onto it. The balance service's readers sum the sets of an account and
-- currency unless they ask for a dimension (FR-DIM-02).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.dimension (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  kind            text        NOT NULL,
  position        smallint,
  name            text        NOT NULL,
  is_active       boolean     NOT NULL DEFAULT true,
  version         integer     NOT NULL DEFAULT 1,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT dimension_pkey PRIMARY KEY (organisation_id, id),
  -- Department and Project once each, and the custom dimensions in places 1 to 8 (FR-DIM-01).
  CONSTRAINT dimension_slot_key UNIQUE NULLS NOT DISTINCT (organisation_id, kind, position),
  CONSTRAINT dimension_name_key UNIQUE (organisation_id, name),
  CONSTRAINT dimension_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT dimension_kind_known CHECK (kind IN ('department', 'project', 'custom')),
  CONSTRAINT dimension_position_custom CHECK ((kind = 'custom') = (position IS NOT NULL) AND (position IS NULL OR position BETWEEN 1 AND 8)),
  CONSTRAINT dimension_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 100),
  CONSTRAINT dimension_version_positive CHECK (version > 0)
);

CREATE TABLE fyniq.dimension_value (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  dimension_id    uuid        NOT NULL,
  code            text        NOT NULL,
  name            text        NOT NULL,
  is_active       boolean     NOT NULL DEFAULT true,
  version         integer     NOT NULL DEFAULT 1,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT dimension_value_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT dimension_value_code_key UNIQUE (organisation_id, dimension_id, code),
  CONSTRAINT dimension_value_dimension_fkey FOREIGN KEY (organisation_id, dimension_id)
    REFERENCES fyniq.dimension (organisation_id, id),
  CONSTRAINT dimension_value_code_format CHECK (code ~ '^[0-9A-Za-z][0-9A-Za-z._/-]{0,29}$'),
  CONSTRAINT dimension_value_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 200),
  CONSTRAINT dimension_value_version_positive CHECK (version > 0)
);
CREATE INDEX dimension_value_dimension_idx ON fyniq.dimension_value (organisation_id, dimension_id);

-- A set's values in ascending order, distinct, at most one of each dimension: one row per combination.
CREATE TABLE fyniq.dimension_set (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  value_ids       uuid[]      NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT dimension_set_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT dimension_set_values_key UNIQUE (organisation_id, value_ids),
  CONSTRAINT dimension_set_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT dimension_set_size CHECK (cardinality(value_ids) BETWEEN 1 AND 10 AND array_position(value_ids, NULL) IS NULL)
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.dimension_set_check() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  n_values     integer;
  n_dimensions integer;
BEGIN
  IF NEW.value_ids <> ARRAY(SELECT DISTINCT v FROM unnest(NEW.value_ids) AS v ORDER BY v) THEN
    RAISE EXCEPTION 'A dimension set lists its values once each, in ascending order'
      USING ERRCODE = 'FY257', HINT = 'Write the set in its canonical form (§11.4)';
  END IF;
  SELECT count(*), count(DISTINCT v.dimension_id) INTO n_values, n_dimensions
    FROM fyniq.dimension_value v
   WHERE v.organisation_id = NEW.organisation_id AND v.id = ANY (NEW.value_ids);
  IF n_values <> cardinality(NEW.value_ids) THEN
    RAISE EXCEPTION 'A dimension set names a value the organisation does not have'
      USING ERRCODE = 'FY257', HINT = 'Name the values of the organisation''s own dimensions (FR-DIM-01)';
  END IF;
  IF n_dimensions <> n_values THEN
    RAISE EXCEPTION 'A dimension set names two values of one dimension'
      USING ERRCODE = 'FY257', HINT = 'A line carries at most one value of each dimension (FR-DIM-02)';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER dimension_set_check BEFORE INSERT ON fyniq.dimension_set
  FOR EACH ROW EXECUTE FUNCTION fyniq.dimension_set_check();

-- Posted lines name their set, so a set never changes or goes (INV-02), whoever asks.
-- +goose StatementBegin
CREATE FUNCTION fyniq.dimension_set_immutable() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A dimension set never changes once written: posted lines name it'
    USING ERRCODE = 'FY002', HINT = 'Write a new set for the new combination';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER dimension_set_immutable BEFORE UPDATE OR DELETE ON fyniq.dimension_set
  FOR EACH ROW EXECUTE FUNCTION fyniq.dimension_set_immutable();
CREATE TRIGGER dimension_set_no_truncate BEFORE TRUNCATE ON fyniq.dimension_set
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.dimension_set_immutable();

ALTER TABLE fyniq.dimension ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.dimension FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.dimension
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.dimension_value ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.dimension_value FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.dimension_value
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.dimension_set ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.dimension_set FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.dimension_set
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.dimension
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('dimension', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.dimension_value
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('dimension', 'dimension_id');

-- ------------------------------------------------------------------ the lines' sets
ALTER TABLE fyniq.journal_line ADD COLUMN dimension_set_id uuid;
ALTER TABLE fyniq.journal_line ADD CONSTRAINT journal_line_dimension_set_fkey FOREIGN KEY (organisation_id, dimension_set_id)
  REFERENCES fyniq.dimension_set (organisation_id, id) NOT VALID;

-- ------------------------------------------------------------------ the cache's key, filled by 000220
ALTER TABLE fyniq.balance ADD COLUMN dimension_set_id uuid;
ALTER TABLE fyniq.balance ADD CONSTRAINT balance_dimension_set_fkey FOREIGN KEY (organisation_id, dimension_set_id)
  REFERENCES fyniq.dimension_set (organisation_id, id) NOT VALID;

GRANT SELECT ON fyniq.dimension, fyniq.dimension_value, fyniq.dimension_set
  TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (dimension_set_id) ON fyniq.journal_line TO fyniq_api, fyniq_worker, fyniq_executor;
GRANT INSERT (value_ids) ON fyniq.dimension_set TO fyniq_api, fyniq_worker, fyniq_executor;
