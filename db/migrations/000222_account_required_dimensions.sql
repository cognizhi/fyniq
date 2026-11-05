-- The dimensions an account requires a value of (PRD-001 FR-DIM-01, rev 0.171; p1-organisation-chart task 3.3, slice
-- 3): a line a person enters on the account names a value of each, or does not post (DIM-05), and a posting the system
-- makes by itself is flagged on Checks instead (Appendix C.2 #60). The application checks a line; the database keeps
-- the list to the organisation's own dimensions, once each.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.account ADD COLUMN required_dimension_ids uuid[] NOT NULL DEFAULT '{}';

-- +goose StatementBegin
CREATE FUNCTION fyniq.account_required_dimensions_check() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  n integer := cardinality(NEW.required_dimension_ids);
BEGIN
  IF n = 0 THEN
    RETURN NEW;
  END IF;
  IF array_position(NEW.required_dimension_ids, NULL) IS NOT NULL
     OR (SELECT count(DISTINCT d) FROM unnest(NEW.required_dimension_ids) AS d) <> n
     OR (SELECT count(*) FROM fyniq.dimension d
          WHERE d.organisation_id = NEW.organisation_id AND d.id = ANY (NEW.required_dimension_ids)) <> n THEN
    RAISE EXCEPTION 'Account % requires a dimension the organisation does not have, or one twice', NEW.code
      USING ERRCODE = 'FY259', HINT = 'Name each of the organisation''s dimensions once (FR-DIM-01)';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER account_required_dimensions_check BEFORE INSERT OR UPDATE OF required_dimension_ids ON fyniq.account
  FOR EACH ROW EXECUTE FUNCTION fyniq.account_required_dimensions_check();

GRANT INSERT (required_dimension_ids), UPDATE (required_dimension_ids) ON fyniq.account TO fyniq_api;
