-- Dimensions and their values kept by the people who keep the chart (PRD-001 FR-DIM-01, Appendix D "Chart of accounts,
-- dimensions"; p1-organisation-chart task 3.3, slice 2: the API).
--
-- The API adds a dimension (Department, Project or a custom one in places 1 to 8) and its values, renames them and puts
-- them out of use. Neither is ever deleted, as dimension sets and the posted lines that name them refer to them, and
-- what a dimension is never changes: its kind and place, a value's dimension and code. The guard below holds that for
-- every role, and each version moves by one, as the API's If-Match reads it.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.dimension_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A dimension or a dimension value is never deleted: dimension sets name it'
      USING ERRCODE = 'FY258', HINT = 'Put it out of use instead';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.version <> 1 OR NOT NEW.is_active THEN
      RAISE EXCEPTION 'A new dimension or dimension value starts in use, at version 1' USING ERRCODE = 'FY258';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION 'A dimension or dimension value changes one version at a time' USING ERRCODE = 'FY258';
  END IF;
  -- One table's columns at a time: PL/pgSQL resolves a field only on the row it is read from.
  IF TG_TABLE_NAME = 'dimension' THEN
    IF (NEW.kind, NEW.position) IS DISTINCT FROM (OLD.kind, OLD.position) THEN
      RAISE EXCEPTION 'A dimension keeps its kind and place' USING ERRCODE = 'FY258',
        HINT = 'Add another dimension for another kind';
    END IF;
  ELSIF (NEW.dimension_id, NEW.code) IS DISTINCT FROM (OLD.dimension_id, OLD.code) THEN
    RAISE EXCEPTION 'A dimension value keeps its dimension and code' USING ERRCODE = 'FY258',
      HINT = 'Add another value for another code';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER dimension_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.dimension
  FOR EACH ROW EXECUTE FUNCTION fyniq.dimension_guard();
CREATE TRIGGER dimension_value_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.dimension_value
  FOR EACH ROW EXECUTE FUNCTION fyniq.dimension_guard();

GRANT INSERT (kind, position, name) ON fyniq.dimension TO fyniq_api;
GRANT UPDATE (name, is_active, version) ON fyniq.dimension TO fyniq_api;
GRANT INSERT (dimension_id, code, name) ON fyniq.dimension_value TO fyniq_api;
GRANT UPDATE (name, is_active, version) ON fyniq.dimension_value TO fyniq_api;

