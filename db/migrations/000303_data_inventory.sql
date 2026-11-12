-- The organisation's own entries in the personal-data inventory, and its quarterly review (PRD-001 CMP-PD-02, CMP-05,
-- CTL-DP-05; rev 0.244; p1-pdpa-retention slice 4, design D3). Fyniq's own classes are its catalogue
-- (compliance/data-inventory.yaml); the organisation adds what it processes outside Fyniq, each entry kept as versions,
-- and the Compliance officer marks the whole inventory reviewed each quarter. Both are only ever added (FY312), each
-- audited in the same transaction (AUD-01), under the "Data protection (PDPA)" row of Appendix D.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.data_inventory_entry (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  entry_id        uuid        NOT NULL,  -- the entry, the same across its versions
  version         integer     NOT NULL,
  what            text        NOT NULL,
  why             text        NOT NULL,
  where_held      text        NOT NULL,
  who             text        NOT NULL,
  retention       text        NOT NULL,
  transfers       text        NOT NULL,
  needs_dpia      boolean     NOT NULL,
  state           text        NOT NULL,  -- open, or retired once the processing stops
  saved_by        text        NOT NULL,
  saved_at        timestamptz NOT NULL,
  CONSTRAINT data_inventory_entry_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT data_inventory_entry_version_once UNIQUE (organisation_id, entry_id, version),
  CONSTRAINT data_inventory_entry_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT data_inventory_entry_version_positive CHECK (version > 0),
  CONSTRAINT data_inventory_entry_texts CHECK (length(btrim(what)) BETWEEN 1 AND 500 AND length(btrim(why)) BETWEEN 1 AND 1000
    AND length(btrim(where_held)) BETWEEN 1 AND 500 AND length(btrim(who)) BETWEEN 1 AND 500 AND length(btrim(retention)) BETWEEN 1 AND 500
    AND length(btrim(transfers)) BETWEEN 1 AND 500),
  CONSTRAINT data_inventory_entry_state_known CHECK (state IN ('open', 'retired')),
  CONSTRAINT data_inventory_entry_saved_by_format CHECK (saved_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

CREATE TABLE fyniq.data_inventory_review (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  quarter         date        NOT NULL,  -- the quarter's first day
  reviewed_by     text        NOT NULL,
  reviewed_at     timestamptz NOT NULL,
  note            text,
  CONSTRAINT data_inventory_review_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT data_inventory_review_once UNIQUE (organisation_id, quarter),
  CONSTRAINT data_inventory_review_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT data_inventory_review_quarter_start CHECK (EXTRACT(DAY FROM quarter) = 1 AND EXTRACT(MONTH FROM quarter) IN (1, 4, 7, 10)),
  CONSTRAINT data_inventory_review_note_length CHECK (length(note) BETWEEN 1 AND 2000),
  CONSTRAINT data_inventory_review_by_format CHECK (reviewed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- An entry's version follows its last; nothing of the inventory ever changes or goes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.data_inventory_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  latest integer;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The data inventory''s entries and reviews never change or go' USING ERRCODE = 'FY312',
      HINT = 'Save the entry''s next version, or retire it (CMP-PD-02).';
  END IF;
  IF TG_TABLE_NAME = 'data_inventory_entry' THEN
    SELECT max(e.version) INTO latest FROM fyniq.data_inventory_entry e
      WHERE e.organisation_id = NEW.organisation_id AND e.entry_id = NEW.entry_id;
    IF NEW.version <> coalesce(latest, 0) + 1 THEN
      RAISE EXCEPTION 'Version % of an inventory entry follows its version %', NEW.version, coalesce(latest, 0) USING ERRCODE = 'FY312';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER data_inventory_entry_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.data_inventory_entry
  FOR EACH ROW EXECUTE FUNCTION fyniq.data_inventory_guard();
CREATE TRIGGER data_inventory_review_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.data_inventory_review
  FOR EACH ROW EXECUTE FUNCTION fyniq.data_inventory_guard();
CREATE TRIGGER data_inventory_entry_no_truncate BEFORE TRUNCATE ON fyniq.data_inventory_entry
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER data_inventory_review_no_truncate BEFORE TRUNCATE ON fyniq.data_inventory_review
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.data_inventory_entry
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('data_inventory_entry', 'entry_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.data_inventory_review
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('data_inventory_review', 'id');

ALTER TABLE fyniq.data_inventory_entry ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.data_inventory_entry FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.data_inventory_entry
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.data_inventory_review ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.data_inventory_review FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.data_inventory_review
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (entry_id, version, what, why, where_held, who, retention, transfers, needs_dpia, state, saved_by, saved_at)
  ON fyniq.data_inventory_entry TO fyniq_api;
GRANT INSERT (quarter, reviewed_by, reviewed_at, note) ON fyniq.data_inventory_review TO fyniq_api;
GRANT SELECT ON fyniq.data_inventory_entry, fyniq.data_inventory_review TO fyniq_api, fyniq_worker, fyniq_readonly;
