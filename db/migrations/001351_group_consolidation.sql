-- The parent's consolidation set-up and its frozen runs (PRD-001 FR-ENT-05, FR-ENT-06; p3-multi-entity design D4 to D6, rev 0.628).
--
-- All four tables are tenant tables of the PARENT organisation, with row-level security, an audit event in the same transaction and the parent's own rights; none reaches into
-- another tenant. group_settings holds the elimination pairs (pairs of the parent's own accounts that mirror each other); group_account_map maps each entity's account code to
-- an account of the parent's chart, with the counterparty entity of an intercompany account; consolidation_rate is the group's own translation rates, a closing and an average per
-- month and currency, insert-only (a newer entry corrects an older); consolidation_run is a frozen run: the figures as read from each entity through its link, the rates, the
-- entities' ledger revisions and period states, and the result, insert-only (a correction is a new run).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.group_settings (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  pairs           jsonb       NOT NULL DEFAULT '[]',  -- [{"a": <account id>, "b": <account id>}]: a normally a debit, b a credit
  version         integer     NOT NULL DEFAULT 1,
  updated_by      uuid        NOT NULL,
  updated_at      timestamptz NOT NULL,
  CONSTRAINT group_settings_pkey PRIMARY KEY (organisation_id),
  CONSTRAINT group_settings_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT group_settings_updater_fkey FOREIGN KEY (updated_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT group_settings_pairs_array CHECK (jsonb_typeof(pairs) = 'array' AND jsonb_array_length(pairs) <= 20),
  CONSTRAINT group_settings_version_positive CHECK (version >= 1)
);

CREATE TABLE fyniq.group_account_map (
  organisation_id      uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                   uuid        NOT NULL DEFAULT uuidv7(),
  entity_row           uuid,                  -- the group_entity row whose account this is; none for the parent's own accounts
  entity_account_code  text        NOT NULL,
  group_account_id     uuid        NOT NULL,  -- the parent's account it maps to
  counterparty_row     uuid,                  -- an intercompany account: the other entity of the group
  counterparty_parent  boolean     NOT NULL DEFAULT false,  -- or the parent itself
  version              integer     NOT NULL DEFAULT 1,
  created_by           uuid        NOT NULL,
  created_at           timestamptz NOT NULL,
  updated_by           uuid        NOT NULL,
  updated_at           timestamptz NOT NULL,
  CONSTRAINT group_account_map_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT group_account_map_entity_fkey FOREIGN KEY (organisation_id, entity_row) REFERENCES fyniq.group_entity (organisation_id, id),
  CONSTRAINT group_account_map_counterparty_fkey FOREIGN KEY (organisation_id, counterparty_row) REFERENCES fyniq.group_entity (organisation_id, id),
  CONSTRAINT group_account_map_account_fkey FOREIGN KEY (organisation_id, group_account_id) REFERENCES fyniq.account (organisation_id, id),
  CONSTRAINT group_account_map_creator_fkey FOREIGN KEY (created_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT group_account_map_updater_fkey FOREIGN KEY (updated_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT group_account_map_code_format CHECK (entity_account_code ~ '^[0-9A-Za-z][0-9A-Za-z.-]{0,19}$'),
  CONSTRAINT group_account_map_one_counterparty CHECK (NOT (counterparty_row IS NOT NULL AND counterparty_parent)),
  CONSTRAINT group_account_map_not_itself CHECK (counterparty_row IS DISTINCT FROM entity_row),
  CONSTRAINT group_account_map_version_positive CHECK (version >= 1)
);
CREATE UNIQUE INDEX group_account_map_entity_code ON fyniq.group_account_map (organisation_id, entity_row, entity_account_code) WHERE entity_row IS NOT NULL;
CREATE UNIQUE INDEX group_account_map_own_code ON fyniq.group_account_map (organisation_id, entity_account_code) WHERE entity_row IS NULL;

CREATE TABLE fyniq.consolidation_rate (
  organisation_id uuid            NOT NULL DEFAULT fyniq.current_org(),
  id              uuid            NOT NULL DEFAULT uuidv7(),
  currency        text            NOT NULL,   -- the entity's currency
  month           date            NOT NULL,   -- the month's first day
  closing         numeric(20, 10) NOT NULL,   -- group currency per one unit of the entity's currency, under RND-RATE
  average         numeric(20, 10) NOT NULL,
  note            text            NOT NULL,   -- where the rates came from, in the person's words
  entered_by      uuid            NOT NULL,
  entered_at      timestamptz     NOT NULL,
  CONSTRAINT consolidation_rate_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT consolidation_rate_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT consolidation_rate_currency_fkey FOREIGN KEY (currency) REFERENCES fyniq.currency (code),
  CONSTRAINT consolidation_rate_enterer_fkey FOREIGN KEY (entered_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT consolidation_rate_month_first CHECK (month = date_trunc('month', month)::date),
  CONSTRAINT consolidation_rate_positive CHECK (closing <> 'NaN' AND closing > 0 AND average <> 'NaN' AND average > 0),
  CONSTRAINT consolidation_rate_note_length CHECK (length(btrim(note)) BETWEEN 1 AND 200)
);
CREATE INDEX consolidation_rate_lookup ON fyniq.consolidation_rate (organisation_id, currency, month, entered_at);

CREATE TABLE fyniq.consolidation_run (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  from_month      date        NOT NULL,
  to_month        date        NOT NULL,
  currency        text        NOT NULL,   -- the group's currency: the parent's base
  provisional     boolean     NOT NULL,   -- an entity's period in the run was not closed when it was read
  inputs          jsonb       NOT NULL,   -- the figures as read, each entity's currency, ledger revision and period states, the rates and the pairs
  result          jsonb       NOT NULL,   -- the engine's result: translated lines, reserves, eliminations, differences, consolidated figures
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  CONSTRAINT consolidation_run_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT consolidation_run_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT consolidation_run_creator_fkey FOREIGN KEY (created_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT consolidation_run_months CHECK (from_month = date_trunc('month', from_month)::date AND to_month = date_trunc('month', to_month)::date AND from_month <= to_month),
  CONSTRAINT consolidation_run_documents CHECK (jsonb_typeof(inputs) = 'object' AND jsonb_typeof(result) = 'object')
);
CREATE INDEX consolidation_run_recent ON fyniq.consolidation_run (organisation_id, created_at DESC, id);

-- A rate and a run are never changed or deleted (FYE12); the mapping and the settings change as the next version, and are never deleted.
-- +goose StatementBegin
CREATE FUNCTION fyniq.group_config_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_TABLE_NAME IN ('consolidation_rate', 'consolidation_run') THEN
    RAISE EXCEPTION 'A translation rate and a consolidation run are kept as made: a correction is a newer entry' USING ERRCODE = 'FYE12',
      HINT = 'Enter the rate again, or run the consolidation again (FR-ENT-05, FR-ENT-06).';
  END IF;
  IF TG_OP IN ('DELETE', 'TRUNCATE') OR NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'The group''s set-up is changed as its next version and never deleted' USING ERRCODE = 'FYE12', HINT = 'Read it again and change it (FR-ENT-05).';
  END IF;
  IF TG_TABLE_NAME = 'group_account_map' AND (NEW.organisation_id, NEW.id, NEW.entity_row, NEW.entity_account_code, NEW.created_by, NEW.created_at)
       IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.entity_row, OLD.entity_account_code, OLD.created_by, OLD.created_at) THEN
    RAISE EXCEPTION 'The identity of a mapping never changes' USING ERRCODE = 'FYE12', HINT = 'Map the account again (FR-ENT-05).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER consolidation_rate_guard BEFORE UPDATE OR DELETE ON fyniq.consolidation_rate FOR EACH ROW EXECUTE FUNCTION fyniq.group_config_guard();
CREATE TRIGGER consolidation_rate_no_truncate BEFORE TRUNCATE ON fyniq.consolidation_rate FOR EACH STATEMENT EXECUTE FUNCTION fyniq.group_config_guard();
CREATE TRIGGER consolidation_run_guard BEFORE UPDATE OR DELETE ON fyniq.consolidation_run FOR EACH ROW EXECUTE FUNCTION fyniq.group_config_guard();
CREATE TRIGGER consolidation_run_no_truncate BEFORE TRUNCATE ON fyniq.consolidation_run FOR EACH STATEMENT EXECUTE FUNCTION fyniq.group_config_guard();
CREATE TRIGGER group_account_map_guard BEFORE UPDATE OR DELETE ON fyniq.group_account_map FOR EACH ROW EXECUTE FUNCTION fyniq.group_config_guard();
CREATE TRIGGER group_account_map_no_truncate BEFORE TRUNCATE ON fyniq.group_account_map FOR EACH STATEMENT EXECUTE FUNCTION fyniq.group_config_guard();
CREATE TRIGGER group_settings_guard BEFORE UPDATE OR DELETE ON fyniq.group_settings FOR EACH ROW EXECUTE FUNCTION fyniq.group_config_guard();
CREATE TRIGGER group_settings_no_truncate BEFORE TRUNCATE ON fyniq.group_settings FOR EACH STATEMENT EXECUTE FUNCTION fyniq.group_config_guard();

ALTER TABLE fyniq.group_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.group_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.group_settings USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.group_settings
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('group_settings', 'organisation_id');
ALTER TABLE fyniq.group_account_map ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.group_account_map FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.group_account_map USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.group_account_map
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('group_account_map', 'id');
ALTER TABLE fyniq.consolidation_rate ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.consolidation_rate FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.consolidation_rate USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.consolidation_rate
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('consolidation_rate', 'id');
ALTER TABLE fyniq.consolidation_run ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.consolidation_run FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.consolidation_run USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.consolidation_run
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('consolidation_run', 'id');

REVOKE ALL ON fyniq.group_settings, fyniq.group_account_map, fyniq.consolidation_rate, fyniq.consolidation_run FROM PUBLIC;
GRANT SELECT ON fyniq.group_settings, fyniq.group_account_map, fyniq.consolidation_rate, fyniq.consolidation_run TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (pairs, updated_by, updated_at) ON fyniq.group_settings TO fyniq_api;
GRANT UPDATE (pairs, version, updated_by, updated_at) ON fyniq.group_settings TO fyniq_api;
GRANT INSERT (entity_row, entity_account_code, group_account_id, counterparty_row, counterparty_parent, created_by, created_at, updated_by, updated_at) ON fyniq.group_account_map TO fyniq_api;
GRANT UPDATE (group_account_id, counterparty_row, counterparty_parent, version, updated_by, updated_at) ON fyniq.group_account_map TO fyniq_api;
GRANT INSERT (currency, month, closing, average, note, entered_by, entered_at) ON fyniq.consolidation_rate TO fyniq_api;
GRANT INSERT (from_month, to_month, currency, provisional, inputs, result, created_by, created_at) ON fyniq.consolidation_run TO fyniq_api;
