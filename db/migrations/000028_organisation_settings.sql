-- Organisation settings (PRD-001 FR-ORG-04, FR-ORG-06, FR-ORG-07, FR-ORG-08, rev 0.34; p1-organisation-chart task 2.3).
--
-- A setting holds its recommended value until it is changed; the catalogue of settings, with each one's
-- recommended value, reason and rules, is code (internal/domain/settings). A change is a row: its value from a
-- date, the key's next version, never edited or deleted, and audited. The value on a day is the latest change
-- in effect then; of two changes from the same day, the later version wins. "Reset to recommended" records the
-- recommended value as a new change.
--
-- Two rules are kept here as well as in the code: the data region is set by the platform, so it never has a
-- row; and the records' retention (§11.9, recommended default 6: 7 years) only lengthens, so no change may drop
-- below the value in effect on its date, or below 7 years when none is, and none may make a change already
-- recorded for a later date shorten it.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.organisation_setting (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  key             text        NOT NULL,
  value           text        NOT NULL,
  effective_from  date        NOT NULL,
  version         integer     NOT NULL,
  recorded_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT organisation_setting_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT organisation_setting_version_key UNIQUE (organisation_id, key, version),
  CONSTRAINT organisation_setting_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT organisation_setting_key_format CHECK (key ~ '^[a-z]+(\.[a-z_]+)+$' AND length(key) <= 60),
  CONSTRAINT organisation_setting_value_length CHECK (length(value) BETWEEN 1 AND 200),
  CONSTRAINT organisation_setting_version_positive CHECK (version > 0),
  CONSTRAINT organisation_setting_region_fixed CHECK (key <> 'data.region')
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.organisation_setting_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  was integer;
BEGIN
  IF NEW.key = 'retention.records_years' THEN
    IF NEW.value !~ '^[0-9]{1,3}$' THEN
      RAISE EXCEPTION 'The records'' retention % is not a whole number of years', NEW.value USING ERRCODE = 'FY091';
    END IF;
    SELECT s.value::integer INTO was FROM fyniq.organisation_setting s
     WHERE s.organisation_id = NEW.organisation_id AND s.key = NEW.key AND s.effective_from <= NEW.effective_from
     ORDER BY s.effective_from DESC, s.version DESC LIMIT 1;
    IF NEW.value::integer < coalesce(was, 7) THEN
      RAISE EXCEPTION 'The records'' retention cannot shorten from % to % years', coalesce(was, 7), NEW.value
        USING ERRCODE = 'FY091',
              HINT = 'Records already kept under a retention keep it (§11.9): keep it or lengthen it.';
    END IF;
    IF EXISTS (SELECT 1 FROM fyniq.organisation_setting s
                WHERE s.organisation_id = NEW.organisation_id AND s.key = NEW.key AND s.effective_from > NEW.effective_from
                  AND s.value::integer < NEW.value::integer) THEN
      RAISE EXCEPTION 'A change to the records'' retention already recorded after % would then shorten it', NEW.effective_from
        USING ERRCODE = 'FY091',
              HINT = 'Records already kept under a retention keep it (§11.9): keep it or lengthen it.';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER organisation_setting_guard BEFORE INSERT ON fyniq.organisation_setting
  FOR EACH ROW EXECUTE FUNCTION fyniq.organisation_setting_guard();

ALTER TABLE fyniq.organisation_setting ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.organisation_setting FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.organisation_setting
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.organisation_setting
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('organisation_setting', 'id');

-- Insert-only: a change is never edited or deleted. Only the API records changes; every role reads them.
GRANT SELECT ON fyniq.organisation_setting TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (key, value, effective_from, version) ON fyniq.organisation_setting TO fyniq_api;
